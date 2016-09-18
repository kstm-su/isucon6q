package main

import (
	"context"
	"time"
)

type Entry struct {
	ID          int
	AuthorID    int
	Keyword     string
	Description string
	UpdatedAt   time.Time
	CreatedAt   time.Time

	Html  string
	Stars []*Star
}

type User struct {
	ID        int
	Name      string
	Salt      string
	Password  string
	CreatedAt time.Time
}

type Star struct {
	ID        int       `json:"id"`
	Keyword   string    `json:"keyword"`
	UserName  string    `json:"user_name"`
	CreatedAt time.Time `json:"created_at"`
}

type EntryWithCtx struct {
	Context context.Context
	Entry   Entry
}

type Keywords struct {
	Contents []string
}

func (k Keywords) Less(i, j int) bool {
	return len(k.Contents[i]) > len(k.Contents[j])
}

func (k Keywords) Len() int {
	return len(k.Contents)
}

func (k Keywords) Swap(i, j int) {
	k.Contents[i], k.Contents[j] = k.Contents[j], k.Contents[i]
}
