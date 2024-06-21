package game

import (
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/exp/rand"
)

const (
	gameID_Length    int           = 12
	game_maxDuration time.Duration = 24 * time.Hour
)

var (
	letterRunes      = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
)

type gameMaster struct {
	gameRouter   *chi.Mux
	gameMap      map[string]*gameData
	gameMapMutex sync.RWMutex
	rng          *rand.Rand
}

func NewGameRouter() *chi.Mux {
	master := gameMaster{
		gameRouter: chi.NewRouter(),
		gameMap:    make(map[string]*gameData),
		rng:        rand.New(rand.NewSource(uint64(time.Now().UnixNano()))),
	}


	return master.gameRouter
}
