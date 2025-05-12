package main

import (
	"github.com/pkg/errors"
)

type Searcher struct {
	//map userId to current search
	userSearches map[int]*Search
}

func NewSearcher() *Searcher {
	return &Searcher{
		userSearches: make(map[int]*Search),
	}
}

func (s *Searcher) AddSearch(userId int) *Search {
	s.userSearches[userId] = &Search{}
	return s.userSearches[userId]
}

func (s *Searcher) GetSearch(userId int) (*Search, error) {
	if search, ok := s.userSearches[userId]; ok {
		return search, nil
	}
	return nil, errors.New("start search first")
}

func (s *Searcher) AddAuthor(userId int, author string) error {
	if search, ok := s.userSearches[userId]; ok {
		search.UpdateAuthor(author)
		return nil
	}
	return errors.New("start search first")
}

func (s *Searcher) AddTitle(userId int, title string) error {
	if search, ok := s.userSearches[userId]; ok {
		search.UpdateTitle(title)
		return nil
	}
	return errors.New("start search first")
}
