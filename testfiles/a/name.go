package task

import (
	"time"
)

//go:generate repo_generator Name github.com/go-generalize/repo_generator/testfiles/a
//go:generate gofmt -w ./

// Name 拡張インデックスあり
type Name struct {
	ID      int64     `datastore:"-" datastore_key:""` // supported type: string, int64, *datastore.Key
	Created time.Time `datastore:"created"`
	Desc    string    `datastore:"description" filter:"l"`  // supported word: e/equal(Default), l/like, p/prefix, TODO s/suffix
	Desc2   string    `datastore:"description2" filter:"p"` // supported word: e/equal(Default), l/like, p/prefix, TODO s/suffix
	Done    bool      `datastore:"done"`
	Count   int       `datastore:"count"`
	Indexes []string  `datastore:"indexes"`
}
