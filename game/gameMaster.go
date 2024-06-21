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
	// Define the length of the GameID in number of runes.
	gameIDLength int = 16

	// The duration a game is kept alive for. After this duration, the game is removed from the game map.
	gameDuration time.Duration = 24 * time.Hour
)

var (
	// The possible letters to use in a random string.
	letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
)

// Struct to manage the individual games. Has functions to create new games, and forward requests to game routers.
type GameMaster struct {
	// The Router for the overall game routes, such as /game/new.
	Router *chi.Mux

	// Map of the games currently alive. Maps from GameID to a gameData.
	gameMap map[string]*GameData

	// Mutex to handle async writing and reading from the game map.
	gameMapMutex sync.RWMutex

	// Random number generator for the game master.
	rng *rand.Rand
}

// Create a new Game Master and return the struct, including the router to be mounted.
func NewGameMaster() *GameMaster {
	master := &GameMaster{
		Router:  chi.NewRouter(),
		gameMap: make(map[string]*GameData),
		rng:     rand.New(rand.NewSource(uint64(time.Now().UnixNano()))),
	}

	// Route to make a new game.
	master.Router.Get("/new", master.newGame)

	// Route to be forward to the individual game with the respective gameID.
	master.Router.HandleFunc("/{gameID}/*", master.handleGame)

	return master
}

// --------------------------------------------------------------------------------
// Utility Functions
// --------------------------------------------------------------------------------

// Create a random string of a specific length, with runes taken from constant array letterRunes.
func (master *GameMaster) randomString(length int) string {
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
		gameID = master.randomString(gameID_Length)

		// If the game ID already exists, try a new ID
		if _, ok := master.gameMap[gameID]; !ok {
			break
		}
	}

	oracleJWTKey := []byte(master.randomString(64))
	oracleJWTExpiry := time.Now().Add(game_maxDuration)
	oracleJWTClaims := &jwt.RegisteredClaims{
		Issuer:    gameID,
		Subject:   "oracle",
		ExpiresAt: jwt.NewNumericDate(oracleJWTExpiry),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, oracleJWTClaims)
	oracleJWTTokenString, _ := token.SignedString(oracleJWTKey)

	data := newGameData(gameID, oracleJWTData{
		key:         oracleJWTKey,
		tokenString: oracleJWTTokenString,
	})
	master.gameMap[gameID] = data
	master.gameMapMutex.Unlock()

	go func() {
		<-time.After(game_maxDuration)
		log.Info().Str("GameID", gameID).Msg("Deleting Game")
		master.gameMapMutex.Lock()
		delete(master.gameMap, gameID)
		master.gameMapMutex.Unlock()
	}()

	log.Info().Str("NewGameID", gameID).Msg("New Game Created")
	http.SetCookie(w, &http.Cookie{
		Name:     "oracleToken-" + gameID,
		Value:    oracleJWTTokenString,
		Expires:  oracleJWTExpiry,
		HttpOnly: true,
	})
	http.Redirect(w, r, fmt.Sprintf("/game/%s/", data.gameID), http.StatusPermanentRedirect)
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
