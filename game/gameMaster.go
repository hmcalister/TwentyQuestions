package game

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
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

	master.router.Get("/new", master.newGame)
	master.router.HandleFunc("/{gameID}/*", master.handleGame)

	return master.router
}

// --------------------------------------------------------------------------------
// Utility Functions
// --------------------------------------------------------------------------------

func (master *gameMaster) randomString(length int) string {
	stringRunes := make([]rune, length)
	for i := range stringRunes {
		stringRunes[i] = letterRunes[master.rng.Intn(len(letterRunes))]
	}
	return string(stringRunes)
}

// --------------------------------------------------------------------------------
// Routing Functions
// --------------------------------------------------------------------------------

func (master *gameMaster) newGame(w http.ResponseWriter, r *http.Request) {
	var gameID string

	master.gameMapMutex.Lock()
	for {
		stringRunes := make([]rune, gameID_Length)
		for i := range stringRunes {
			stringRunes[i] = letterRunes[master.rng.Intn(len(letterRunes))]
		}
		gameID = string(stringRunes)

		// If the game ID already exists, try a new ID
		if _, ok := master.gameMap[gameID]; !ok {
			break
		}
	}

	data := newGameData(gameID)
	master.gameMap[gameID] = data
	master.gameMapMutex.Unlock()

	log.Info().Interface("NewGameData", data).Msg("New Game Created")
	http.Redirect(w, r, fmt.Sprintf("/game/%s/", data.GameID), http.StatusPermanentRedirect)
}

func (master *gameMaster) handleGame(w http.ResponseWriter, r *http.Request) {
	gameIDParam := chi.URLParam(r, "gameID")
	master.gameMapMutex.RLock()
	defer master.gameMapMutex.RUnlock()
	targetGameData, ok := master.gameMap[gameIDParam]

	// If the requested GameID does not exist, return a 404
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	targetGameData.router.ServeHTTP(w, r)
}
