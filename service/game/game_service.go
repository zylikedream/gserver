package game

import "gserver/core/gxyservice"

type gameService struct {
	gxyservice.InnerService
}

var gamesvc = NewGameService()

func GameService() *gameService {
	return gamesvc
}

func NewGameService() *gameService {
	return &gameService{}
}

func (a *gameService) Name() string {
	return ""
}
