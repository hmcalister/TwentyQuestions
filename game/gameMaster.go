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

// http handler to create a new game. Game is added atomically to the game map, and hence is accessible from /{gameID}/*.
func (master *GameMaster) newGame(w http.ResponseWriter, r *http.Request) {
	var gameID string

	// Lock the entire gameMapMutex until we are finished making the game, to avoid the (slim) chance we generate the same ID twice.
	master.gameMapMutex.Lock()
	for {
		gameID = master.randomString(gameIDLength)

		// If the game ID already exists, try a new ID
		if _, ok := master.gameMap[gameID]; !ok {
			break
		}
	}

	// Make a placeholder item so we can overwrite it later, but unlock the mutex ASAP.
	master.gameMap[gameID] = &GameData{}
	master.gameMapMutex.Unlock()

	oracleJWTKey := []byte(master.randomString(64))
	oracleJWTExpiry := time.Now().Add(gameDuration)
	oracleJWTClaims := &jwt.RegisteredClaims{
		Issuer:    gameID,
		Subject:   "oracle",
		ExpiresAt: jwt.NewNumericDate(oracleJWTExpiry),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, oracleJWTClaims)
	oracleJWTTokenString, _ := token.SignedString(oracleJWTKey)

	data := newGameData(gameID, oracleJWTKey)
	master.gameMap[gameID] = data

	// Start a goroutine to delete the game after a set duration.
	go func() {
		<-time.After(gameDuration)
		log.Info().Str("GameID", gameID).Msg("Deleting Game")
		master.gameMapMutex.Lock()
		delete(master.gameMap, gameID)
		master.gameMapMutex.Unlock()
	}()

	log.Info().Str("NewGameID", gameID).Msg("New Game Created")
	http.SetCookie(w, &http.Cookie{
		Name:     gameID,
		Value:    oracleJWTTokenString,
		Expires:  oracleJWTExpiry,
		HttpOnly: true,
	})
	http.Redirect(w, r, fmt.Sprintf("/game/%s/", data.gameID), http.StatusPermanentRedirect)
}

// http handler to forward requests to a specific game -- or 404 if the gameID is not in the map.
func (master *GameMaster) handleGame(w http.ResponseWriter, r *http.Request) {
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
