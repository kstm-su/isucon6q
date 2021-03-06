package main

import (
	"time"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sort"

	"github.com/Songmu/strrand"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/unrolled/render"
)

const (
	sessionName   = "isuda_session"
	sessionSecret = "tonymoris"
)

var (
	isutarEndpoint string
	isupamEndpoint string

	baseUrl *url.URL
	db      *sql.DB
	re      *render.Render
	store   *sessions.CookieStore

	errInvalidUser = errors.New("Invalid User")

	allUsers map[string]*User
	allEntries []*Entry
	keywordEntries map[string]*Entry
	keywords []string
	allStars map[string][]*Star
	htmlVersion int
)

//star関連
func starsHandler2(keyword string)[]*Star {
	res := allStars[keyword]
	return res
}

func starsHandler(w http.ResponseWriter, r *http.Request) {
        keyword := r.FormValue("keyword")
	stars := starsHandler2(keyword)
        re.JSON(w, http.StatusOK, map[string][]*Star{
                "result": stars,
        })
}

//star関連
func starsPostHandler(w http.ResponseWriter, r *http.Request) {
        keyword := r.FormValue("keyword")
		if keywordEntries[keyword] == nil {
                notFound(w)
                return
        }

        user := r.FormValue("user")
        res, err := db.Exec(`INSERT INTO star (keyword, user_name, created_at) VALUES (?, ?, NOW())`, keyword, user)
		id, _ := res.LastInsertId()
		allStars[keyword] = append(allStars[keyword], &Star{
			ID: int(id),
			Keyword: keyword,
			UserName: user,
			CreatedAt: time.Now(),
		})
        panicIf(err)

        re.JSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func setName(w http.ResponseWriter, r *http.Request) error {
	session := getSession(w, r)
	userID, ok := session.Values["user_id"]
	if !ok {
		return nil
	}
	setContext(r, "user_id", userID)
	//row := db.QueryRow(`SELECT name FROM user WHERE id = ?`, userID)
	//user := User{}
	//err := row.Scan(&user.Name)
	//if err != nil {
	//	if err == sql.ErrNoRows {
	//		return errInvalidUser
	//	}
	//	panicIf(err)
	//}
	var user *User = nil
	for _, u := range allUsers {
		if u.ID == userID {
			user = u
		}
	}
	if user == nil {
		return errInvalidUser
	}
	setContext(r, "user_name", user.Name)
	return nil
}

func authenticate(w http.ResponseWriter, r *http.Request) error {
	if u := getContext(r, "user_id"); u != nil {
		return nil
	}
	return errInvalidUser
}

func load() {
	allUsers = make(map[string]*User)
	rows, _ := db.Query("SELECT * FROM user")
	for rows.Next() {
		u := User{}
		err := rows.Scan(&u.ID, &u.Name, &u.Salt, &u.Password, &u.CreatedAt)
		panicIf(err)
		allUsers[u.Name] = &u
	}
	rows.Close()

	rows, _ = db.Query("SELECT * FROM entry ORDER BY updated_at")
	allEntries = make([]*Entry, 0)
	keywordEntries = make(map[string]*Entry)
	keywords = make([]string, 0)
	for rows.Next() {
		e := Entry{}
		err := rows.Scan(&e.ID, &e.AuthorID, &e.Keyword, &e.Description, &e.UpdatedAt, &e.CreatedAt)
		panicIf(err)
		e.HTMLVersion = -1
		e.Stars = make([]*Star, 0)
		allEntries = append(allEntries, &e)
		keywordEntries[e.Keyword] = &e
		keywords = append(keywords, e.Keyword)
	}
	rows.Close()
	sort.Sort(Keywords{keywords})

	allStars = make(map[string][]*Star)
	rows, _ = db.Query("SELECT * FROM star")
	for rows.Next() {
		s := Star{}
		err := rows.Scan(&s.ID, &s.Keyword, &s.UserName, &s.CreatedAt)
		panicIf(err)
		if allStars[s.Keyword] == nil {
			allStars[s.Keyword] = make([]*Star, 0)
		}
		allStars[s.Keyword] = append(allStars[s.Keyword], &s)
	}
	rows.Close()
}

func initializeHandler(w http.ResponseWriter, r *http.Request) {
	htmlVersion = 0
	_, err := db.Exec(`DELETE FROM entry WHERE id > 7101`)
	panicIf(err)

	load()

	_, err = db.Exec("TRUNCATE star")
        panicIf(err)

	re.JSON(w, http.StatusOK, map[string]string{"result": "ok"})
}

func topHandler(w http.ResponseWriter, r *http.Request) {
	if err := setName(w, r); err != nil {
		forbidden(w)
		return
	}

	perPage := 10
	p := r.URL.Query().Get("page")
	if p == "" {
		p = "1"
	}
	page, _ := strconv.Atoi(p)
	entries := allEntries[perPage*(page-1) : perPage*page]
	for _, e := range entries {
		e.Stars = loadStars(e.Keyword)
		//e.Html = htmlify(w, r, e.Description)
		if e.HTMLVersion != htmlVersion {
			e.Html = htmlify(e.Description)
			e.HTMLVersion = htmlVersion
		}
	}

	totalEntries := len(allEntries)

	lastPage := int(math.Ceil(float64(totalEntries) / float64(perPage)))
	pages := make([]int, 0, 10)
	start := int(math.Max(float64(1), float64(page-5)))
	end := int(math.Min(float64(lastPage), float64(page+5)))
	for i := start; i <= end; i++ {
		pages = append(pages, i)
	}

	re.HTML(w, http.StatusOK, "index", struct {
		Context  context.Context
		Entries  []*Entry
		Page     int
		LastPage int
		Pages    []int
	}{
		r.Context(), entries, page, lastPage, pages,
	})
}

func robotsHandler(w http.ResponseWriter, r *http.Request) {
	notFound(w)
}

func keywordPostHandler(w http.ResponseWriter, r *http.Request) {
	if err := setName(w, r); err != nil {
		forbidden(w)
		return
	}
	if err := authenticate(w, r); err != nil {
		forbidden(w)
		return
	}

	keyword := r.FormValue("keyword")
	if keyword == "" {
		badRequest(w)
		return
	}
	userID := getContext(r, "user_id").(int)
	description := r.FormValue("description")

	if isSpamContents(description) || isSpamContents(keyword) {
		http.Error(w, "SPAM!", http.StatusBadRequest)
		return
	}
	res, err := db.Exec(`
		INSERT INTO entry (author_id, keyword, description, created_at, updated_at)
		VALUES (?, ?, ?, NOW(), NOW())
		ON DUPLICATE KEY UPDATE
		author_id = ?, keyword = ?, description = ?, updated_at = NOW()
	`, userID, keyword, description, userID, keyword, description)
	createdAt := time.Now()
	if keywordEntries[keyword] != nil {
		createdAt = keywordEntries[keyword].CreatedAt
	}
	id, _ := res.LastInsertId()
	e := Entry{
		ID: int(id),
		AuthorID: userID,
		Keyword: keyword,
		Description: description,
		UpdatedAt: time.Now(),
		CreatedAt: createdAt,
	}
	allEntries = append([]*Entry{&e}, allEntries...)
	keywordEntries[keyword] = &e
	keywords = append(keywords, keyword)
	allStars[keyword] = make([]*Star, 0)
	sort.Sort(Keywords{keywords})
	htmlVersion++
	panicIf(err)
	http.Redirect(w, r, "/", http.StatusFound)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if err := setName(w, r); err != nil {
		forbidden(w)
		return
	}

	re.HTML(w, http.StatusOK, "authenticate", struct {
		Context context.Context
		Action  string
	}{
		r.Context(), "login",
	})
}

func loginPostHandler(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	user := allUsers[name]
	if user == nil || user.Password != fmt.Sprintf("%x", sha1.Sum([]byte(user.Salt+r.FormValue("password")))) {
		forbidden(w)
		return
	}
	session := getSession(w, r)
	session.Values["user_id"] = user.ID
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	session := getSession(w, r)
	session.Options = &sessions.Options{MaxAge: -1}
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if err := setName(w, r); err != nil {
		forbidden(w)
		return
	}

	re.HTML(w, http.StatusOK, "authenticate", struct {
		Context context.Context
		Action  string
	}{
		r.Context(), "register",
	})
}

func registerPostHandler(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	pw := r.FormValue("password")
	if name == "" || pw == "" {
		badRequest(w)
		return
	}
	userID := register(name, pw)
	session := getSession(w, r)
	session.Values["user_id"] = userID
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func register(user string, pass string) int64 {
	salt, err := strrand.RandomString(`....................`)
	panicIf(err)
	password := fmt.Sprintf("%x", sha1.Sum([]byte(salt+pass)))
	res, err := db.Exec(`INSERT INTO user (name, salt, password, created_at) VALUES (?, ?, ?, NOW())`,
		user, salt, password)
	panicIf(err)
	lastInsertID, _ := res.LastInsertId()
	allUsers[user] = &User{
		ID: int(lastInsertID),
		Name: user,
		Salt: salt,
		Password: password,
		CreatedAt: time.Now(),
	}
	return lastInsertID
}

func keywordByKeywordHandler(w http.ResponseWriter, r *http.Request) {
	if err := setName(w, r); err != nil {
		forbidden(w)
		return
	}

	keyword := mux.Vars(r)["keyword"]
	e := keywordEntries[keyword]
	if e == nil {
		notFound(w)
		return
	}
	//e.Html = htmlify(w, r, e.Description)
	if e.HTMLVersion != htmlVersion {
		e.Html = htmlify(e.Description)
		e.HTMLVersion = htmlVersion
	}
	e.Stars = loadStars(e.Keyword)

	re.HTML(w, http.StatusOK, "keyword", struct {
		Context context.Context
		Entry   *Entry
	}{
		r.Context(), e,
	})
}

func keywordByKeywordDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if err := setName(w, r); err != nil {
		forbidden(w)
		return
	}
	if err := authenticate(w, r); err != nil {
		forbidden(w)
		return
	}

	keyword := mux.Vars(r)["keyword"]
	if keyword == "" {
		badRequest(w)
		return
	}
	if r.FormValue("delete") == "" {
		badRequest(w)
		return
	}
	for i, k := range keywords {
		if k == keyword {
			copy(keywords[i:], keywords[i+1:])
			keywords = keywords[:len(keywords)-1]
			break
		}
	}
	if keywordEntries[keyword] == nil {
		notFound(w)
		return
	}
	_, err := db.Exec(`DELETE FROM entry WHERE keyword = ?`, keyword)
	panicIf(err)
	_, err = db.Exec(`DELETE FROM star WHERE keyword = ?`, keyword)
	panicIf(err)
	http.Redirect(w, r, "/", http.StatusFound)
}

//func htmlify(w http.ResponseWriter, r *http.Request, content string) string {
func htmlify(content string) string {
	if content == "" {
		return ""
	}
	content = html.EscapeString(content)
	content = strings.Replace(content, "@", "@@", -1)
	kw := make([]string, 0)
	for _, k := range keywords {
		tmp := strings.Replace(content, k, "@(" + strconv.Itoa(len(kw)) + ")", -1)
		if tmp != content {
			kw = append(kw, k)
			content = tmp
		}
	}
	for i := len(kw) - 1; i >= 0; i-- {
		k := kw[i]
		//url, _ := r.URL.Parse(baseUrl.String() + "/keyword/" + pathURIEscape(k))
		url := "http://13.78.126.110/keyword/" + pathURIEscape(k)
		//link := "<a href=\"" + url.String() + "\">" + html.EscapeString(k) + "</a>"
		link := "<a href=\"" + url + "\">" + html.EscapeString(k) + "</a>"
		content = strings.Replace(content, "@(" + strconv.Itoa(i) + ")", link, -1)
	}
	content = strings.Replace(content, "@@", "@", -1)
	return strings.Replace(content, "\n", "<br />\n", -1)
}

func loadStars(keyword string) []*Star {
	return starsHandler2(keyword)
}

func isSpamContents(content string) bool {
	v := url.Values{}
	v.Set("content", content)
	resp, err := http.PostForm(isupamEndpoint, v)
	panicIf(err)
	defer resp.Body.Close()

	var data struct {
		Valid bool `json:valid`
	}
	err = json.NewDecoder(resp.Body).Decode(&data)
	panicIf(err)
	return !data.Valid
}

func getContext(r *http.Request, key interface{}) interface{} {
	return r.Context().Value(key)
}

func setContext(r *http.Request, key, val interface{}) {
	if val == nil {
		return
	}

	r2 := r.WithContext(context.WithValue(r.Context(), key, val))
	*r = *r2
}

func getSession(w http.ResponseWriter, r *http.Request) *sessions.Session {
	session, _ := store.Get(r, sessionName)
	return session
}

func main() {
	host := os.Getenv("ISUDA_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	portstr := os.Getenv("ISUDA_DB_PORT")
	if portstr == "" {
		portstr = "3306"
	}
	port, err := strconv.Atoi(portstr)
	if err != nil {
		log.Fatalf("Failed to read DB port number from an environment variable ISUDA_DB_PORT.\nError: %s", err.Error())
	}
	user := os.Getenv("ISUDA_DB_USER")
	if user == "" {
		//user = "root"
		user = "isucon"
	}
	//password := os.Getenv("ISUDA_DB_PASSWORD")
	password := "isucon"
	dbname := os.Getenv("ISUDA_DB_NAME")
	if dbname == "" {
		dbname = "isuda"
	}

	db, err = sql.Open("mysql", fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?loc=Local&parseTime=true",
		user, password, host, port, dbname,
	))
	if err != nil {
		log.Fatalf("Failed to connect to DB: %s.", err.Error())
	}
	db.Exec("SET SESSION sql_mode='TRADITIONAL,NO_AUTO_VALUE_ON_ZERO,ONLY_FULL_GROUP_BY'")
	db.Exec("SET NAMES utf8mb4")
	load()

	isutarEndpoint = os.Getenv("ISUTAR_ORIGIN")
	if isutarEndpoint == "" {
		isutarEndpoint = "http://localhost:5001"
	}
	isupamEndpoint = os.Getenv("ISUPAM_ORIGIN")
	if isupamEndpoint == "" {
		isupamEndpoint = "http://localhost:5050"
	}

	store = sessions.NewCookieStore([]byte(sessionSecret))

	re = render.New(render.Options{
		Directory: "views",
		Funcs: []template.FuncMap{
			{
				"url_for": func(path string) string {
					return baseUrl.String() + path
				},
				"title": func(s string) string {
					return strings.Title(s)
				},
				"raw": func(text string) template.HTML {
					return template.HTML(text)
				},
				"add": func(a, b int) int { return a + b },
				"sub": func(a, b int) int { return a - b },
				"entry_with_ctx": func(entry Entry, ctx context.Context) *EntryWithCtx {
					return &EntryWithCtx{Context: ctx, Entry: entry}
				},
			},
		},
	})

	r := mux.NewRouter()
	r.HandleFunc("/", myHandler(topHandler))
	r.HandleFunc("/initialize", myHandler(initializeHandler)).Methods("GET")
	r.HandleFunc("/robots.txt", myHandler(robotsHandler))
	r.HandleFunc("/keyword", myHandler(keywordPostHandler)).Methods("POST")

	l := r.PathPrefix("/login").Subrouter()
	l.Methods("GET").HandlerFunc(myHandler(loginHandler))
	l.Methods("POST").HandlerFunc(myHandler(loginPostHandler))
	r.HandleFunc("/logout", myHandler(logoutHandler))

	g := r.PathPrefix("/register").Subrouter()
	g.Methods("GET").HandlerFunc(myHandler(registerHandler))
	g.Methods("POST").HandlerFunc(myHandler(registerPostHandler))

	k := r.PathPrefix("/keyword/{keyword}").Subrouter()
	k.Methods("GET").HandlerFunc(myHandler(keywordByKeywordHandler))
	k.Methods("POST").HandlerFunc(myHandler(keywordByKeywordDeleteHandler))

//star
        s := r.PathPrefix("/stars").Subrouter()
        s.Methods("GET").HandlerFunc(myHandler(starsHandler))
        s.Methods("POST").HandlerFunc(myHandler(starsPostHandler))

	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./public/")))
	//app, _ := net.Listen("unix", "/var/run/isuda.sock")
	//log.Fatal(http.Serve(app, r))
	log.Fatal(http.ListenAndServe(":5000", r))
}
