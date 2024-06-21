package game

import (
	"fmt"
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
	letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
)

type gameMaster struct {
	router       *chi.Mux
	gameMap      map[string]*gameData
	gameMapMutex sync.RWMutex
	rng          *rand.Rand
}

func NewGameRouter() *chi.Mux {
	master := gameMaster{
		router:  chi.NewRouter(),
		gameMap: make(map[string]*gameData),
		rng:     rand.New(rand.NewSource(uint64(time.Now().UnixNano()))),
	}

	master.gameRouter.Get("/new", master.newGame)
	master.gameRouter.Get("/{gameID}", master.handleGame)

	return master.gameRouter
}

func (master *gameMaster) newGameID() string {
	stringRunes := make([]rune, gameID_Length)
	for i := range stringRunes {
		stringRunes[i] = letterRunes[master.rng.Intn(len(letterRunes))]
	}
	return string(stringRunes)
}

func (master *gameMaster) newGame(w http.ResponseWriter, r *http.Request) {
	var newGameID string

	master.gameMapMutex.Lock()
	for {
		newGameID = master.newGameID()

		// If the game ID already exists, try a new ID
		if _, ok := master.gameMap[newGameID]; !ok {
			break
		}
	}

	newGameData := &gameData{
		GameID: newGameID,
	}
	master.gameMap[newGameID] = newGameData
	master.gameMapMutex.Unlock()

	log.Info().Interface("NewGameData", newGameData).Msg("New Game Created")
	http.Redirect(w, r, fmt.Sprintf("/game/%s", newGameData.GameID), http.StatusPermanentRedirect)
}

func (master *gameMaster) handleGame(w http.ResponseWriter, r *http.Request) {
	gameIDParam := chi.URLParam(r, "gameID")

	master.gameMapMutex.RLock()
	targetGameData, ok := master.gameMap[gameIDParam]
	master.gameMapMutex.RUnlock()

	// If the requested GameID does not exist, return a 404
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	gameBaseTemplate.Execute(w, targetGameData)
}
