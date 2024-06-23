package game

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/microcosm-cc/bluemonday"
	"github.com/rs/zerolog/log"
)

var (
	// Templates for Game page.
	gameTemplate = template.Must(template.ParseFiles("templates/gameBase.html", "templates/gameItem.html", "templates/gameOver.html"))
)

// Enum for gameState, determining what is required next.
type gameStateEnum int

const (
	gameState_AwaitingQuestion gameStateEnum = iota
	gameState_AwaitingAnswer   gameStateEnum = iota
	gameState_GameOver         gameStateEnum = iota
)

// --------------------------------------------------------------------------------
// JWT Data and Methods
// --------------------------------------------------------------------------------

// Check a request for the JWT to authenticate the oracle.
func (data *GameData) checkRequestFromOracle(r *http.Request) bool {
	// Ensure cookie actually exits

	tokenCookie, err := r.Cookie(data.gameID)
	if err != nil {
		if err == http.ErrNoCookie {
			log.Debug().Msg("Oracle JWT Check - No Token Cookie")
			return false
		}
		log.Debug().Msg("Oracle JWT Check - Error Reading Cookie")
		return false
	}

	// Get claims from cookie by decoding with key

	tokenString := tokenCookie.Value
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return data.oracleJWTKey, nil
	})

	// log.Debug().Interface("JWT Claims", claims).Msg("Oracle JWT Check")

	// Ensure decoding did not fail

	if err != nil {
		if err == jwt.ErrSignatureInvalid {
			log.Debug().Msg("Oracle JWT Check - Invalid Signature")
			return false
		}
		log.Debug().Msg("Oracle JWT Check - Other Token Parse Error")
		return false
	}

	// Ensure the token is still valid (e.g. not expired)

	if !token.Valid {
		log.Debug().Msg("Oracle JWT Check - Token Invalid")
		return false
	}

	// Ensure claims match expected

	if claims.Issuer != data.gameID || claims.Subject != "oracle" {
		return false
	}

	return true
}

// Middleware to wrap the check for oracleJWT, setting a context value in the request for IsOracle.
func (data *GameData) checkRequestFromOracleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "IsOracle", data.checkRequestFromOracle(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --------------------------------------------------------------------------------
// Game Data struct
// --------------------------------------------------------------------------------

// Question and Answer pairs -- kept for ease of template parsing.
type questionAnswerPair struct {
	Index    int
	Question string
	Answer   string
}

// Server Sent Event client -- to return the question and answers to the clients as responses roll in.
type sseClient struct {
	// Context, with cancel indicating if the client has left.
	context context.Context

	cancelFunc context.CancelFunc

	// Channel to write responses back to user (ensure no newlines!!). Must not be sent data if context is done.
	responsesChannel chan string
}

// Data representing an individual game.
type GameData struct {
	gameID string

	// Signing key for the oracle JWT.
	oracleJWTKey []byte

	router *chi.Mux

	// Current state of game -- switches between awaiting question and awaiting answer.
	gameState gameStateEnum

	// Mutex to ensure atomic read and write of the game state -- prevents double questions in edge cases.
	gameStateMutex sync.Mutex

	// All question answer pairs in this game.
	questionAnswerPairs []questionAnswerPair

	// String to store current HTML of question answer pairs, to avoid recomputation for every SSE client.
	allResponsesHTML string

	// BlueMonday HTML Sanitizer -- ensures user input is clean before sending to other clients.
	htmlSanitizer *bluemonday.Policy

	// Array of all sseClients -- pruned of closed clients when next event is sent.
	sseClients []*sseClient

	// Mutex to ensure atomic handling of SSE clients -- we don't want to accidentally miss a client!
	sseClientsMutex sync.Mutex
}

// Create a new game data, including registering routes on router.
func newGameData(gameID string, oracleJWTKey []byte, htmlSanitizer *bluemonday.Policy) *GameData {
	data := &GameData{
		gameID:              gameID,
		oracleJWTKey:        oracleJWTKey,
		router:              chi.NewRouter(),
		gameState:           gameState_AwaitingQuestion,
		questionAnswerPairs: make([]questionAnswerPair, 0),
		allResponsesHTML:    "",
		htmlSanitizer:       htmlSanitizer,
		sseClients:          make([]*sseClient, 0),
	}

	// Always check if request is from the oracle.
	data.router.Use(data.checkRequestFromOracleMiddleware)

	data.router.Get("/"+data.gameID+"/", data.renderGameBase)
	data.router.Post("/"+data.gameID+"/submitResponse", data.handleNewResponse)
	data.router.Get("/"+data.gameID+"/responsesSourceSSE", data.responsesSourceSSE)
	data.router.Get("/"+data.gameID+"/oracleVerdictCorrect", data.oracleVerdictCorrect)
	data.router.Get("/"+data.gameID+"/oracleVerdictIncorrect", data.oracleVerdictIncorrect)

	return data
}

func (data *GameData) sendClientsResponseHTML() {
	// Loop over clients, splice out any that are closed, send to any that are alive.

	log.Debug().Msg("SENDING CLIENTS")

	data.sseClientsMutex.Lock()
	defer data.sseClientsMutex.Unlock()

	// Note this loop does NOT always increment i, as sometimes we splice out a done client and must repeat that index.
	// If we splice out the last client, the i will now be equal to len(data.sseClients) so the loop will terminate, not overrun its bounds
	for i := 0; i < len(data.sseClients); {
		currentClient := data.sseClients[i]
		select {
		// If this client is done, the context is cancelled, and we can close the response channel to clean up some goroutines.
		case <-currentClient.context.Done():
			close(currentClient.responsesChannel)
			// Splice out the done client with the end client. Then remove the end client.
			// This requires us to look at the current index again, so don't update i.
			data.sseClients[i] = data.sseClients[len(data.sseClients)-1]
			data.sseClients = data.sseClients[:len(data.sseClients)-1]
		default:
			// This client is still alive, send them the new HTML and move to the next client.
			currentClient.responsesChannel <- data.allResponsesHTML
			i += 1
		}
	}
}

// Add a new question. Returns an error if the game is currently awaiting an answer instead.
// Change of state is handled internally by this function.
func (data *GameData) addNextQuestion(question string) error {
	// Ensure the game state is checked atomically.
	//
	// If two clients submit a question at the same time, one will get the lock and the question, and the other is turned away.
	data.gameStateMutex.Lock()
	defer data.gameStateMutex.Unlock()

	if data.gameState != gameState_AwaitingQuestion {
		return errors.New("not currently awaiting question")
	}

	nextQApair := questionAnswerPair{
		Index:    len(data.questionAnswerPairs) + 1,
		Question: question,
	}
	data.questionAnswerPairs = append(data.questionAnswerPairs, nextQApair)
	data.gameState = gameState_AwaitingAnswer
	return nil
}

// Add a new answer. Returns an error if the game is currently awaiting a question instead.
// Change of state is handled internally by this function.
func (data *GameData) addNextAnswer(answer string) error {
	// Ensure the game state is checked atomically.
	//
	// If the oracle submits two answers at the same time, one will get the lock and the other is turned away.
	data.gameStateMutex.Lock()
	defer data.gameStateMutex.Unlock()

	if data.gameState != gameState_AwaitingAnswer {
		return errors.New("not currently awaiting answer")
	}

	data.questionAnswerPairs[len(data.questionAnswerPairs)-1].Answer = answer
	data.gameState = gameState_AwaitingQuestion
	return nil
}

func (data *GameData) gameCleanup() {
	data.sseClientsMutex.Lock()
	defer data.sseClientsMutex.Unlock()

	// Note this loop does NOT always increment i, as sometimes we splice out a done client and must repeat that index.
	// If we splice out the last client, the i will now be equal to len(data.sseClients) so the loop will terminate, not overrun its bounds
	for i := len(data.sseClients) - 1; i >= 0; i-- {
		currentClient := data.sseClients[i]
		close(currentClient.responsesChannel)
		select {
		case <-currentClient.context.Done():
		default:
			currentClient.cancelFunc()
		}
	}

	data.sseClients = make([]*sseClient, 0)
}

// --------------------------------------------------------------------------------
// Routing Functions
// --------------------------------------------------------------------------------

// Data to be passed to gameBase.html template
type gameBaseTemplateData struct {
	GameID   string
	IsOracle bool
}

// Render the game base -- should the first call to the game router.
func (data *GameData) renderGameBase(w http.ResponseWriter, r *http.Request) {
	// Render the template with all current data. Ensures late players still get all previous questions and answers.
	err := gameTemplate.ExecuteTemplate(w, "gameBase.html", gameBaseTemplateData{
		GameID:   data.gameID,
		IsOracle: r.Context().Value("IsOracle").(bool),
	})
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game base template")
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// Handle a response in the game -- this function handles both guesser and oracle responses.
//
// This function also updates the questionAnswerPairs and allResponsesHTML fields, and sends this data to all SSE clients.
func (data *GameData) handleNewResponse(w http.ResponseWriter, r *http.Request) {
	response := r.FormValue("response")
	// response := data.htmlSanitizer.Sanitize(r.FormValue("response"))
	log.Debug().Str("Game ID", data.gameID).Str("Response", response).Msg("Game Response")

	if len(response) == 0 || data.gameState == gameState_GameOver {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	isOracle := r.Context().Value("IsOracle").(bool)
	if isOracle {
		err := data.addNextAnswer(response)
		if err != nil {
			log.Debug().Msg("Not Oracles Turn!")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else {
		err := data.addNextQuestion(response)
		if err != nil {
			log.Debug().Msg("Not Guessers Turn!")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	}

	w.WriteHeader(http.StatusOK)

	// Update the allResponseHTML field to send to all sse clients

	var updatedResponsesBytes bytes.Buffer
	err := gameTemplate.ExecuteTemplate(&updatedResponsesBytes, "gameItem.html", data.questionAnswerPairs)
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game item template")
		return
	}
	data.allResponsesHTML = updatedResponsesBytes.String()
	data.allResponsesHTML = strings.ReplaceAll(data.allResponsesHTML, "\n", "")
	data.sendClientsResponseHTML()
}

// SSE endpoint
func (data *GameData) responsesSourceSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Make a new SSE client using the request context and responses channel
	responsesChannel := make(chan string)
	newClient := &sseClient{
		context:          r.Context(),
		responsesChannel: responsesChannel,
	}

	// Atomically add the new client to the clients list -- mutex avoids appending to list while splicing out list in handleResponse handler.
	data.sseClientsMutex.Lock()
	data.sseClients = append(data.sseClients, newClient)
	data.sseClientsMutex.Unlock()

	// This goroutine terminates when responsesChannel is closed, which is handled in handleResponse handler when the client context is cancelled (client leaves).
	go func() {
		for responsesHTML := range responsesChannel {
			fmt.Fprintf(w, "data: %s\n\n", responsesHTML)
			w.(http.Flusher).Flush()
		}
	}()

	// Send the current allResponsesHTML to initialize the client
	responsesChannel <- data.allResponsesHTML

	// Block return until the response is canceled -- ensures the client only makes ONE connection, rather than continually polling.
	<-r.Context().Done()
}

// Used when the oracle ends the game with a correct verdict
func (data *GameData) oracleVerdictCorrect(w http.ResponseWriter, r *http.Request) {
	isOracle := r.Context().Value("IsOracle").(bool)
	if !isOracle {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if data.gameState == gameState_GameOver {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	data.gameState = gameState_GameOver

	var updatedResponsesBytes bytes.Buffer
	err := gameTemplate.ExecuteTemplate(&updatedResponsesBytes, "gameOver.html", true)
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game over template")
		return
	}
	data.allResponsesHTML += updatedResponsesBytes.String()
	data.allResponsesHTML = strings.ReplaceAll(data.allResponsesHTML, "\n", "")
	data.sendClientsResponseHTML()
}

// Used when the oracle ends the game with an incorrect verdict
func (data *GameData) oracleVerdictIncorrect(w http.ResponseWriter, r *http.Request) {
	isOracle := r.Context().Value("IsOracle").(bool)
	if !isOracle {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if data.gameState == gameState_GameOver {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	data.gameState = gameState_GameOver

	var updatedResponsesBytes bytes.Buffer
	err := gameTemplate.ExecuteTemplate(&updatedResponsesBytes, "gameOver.html", false)
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game over template")
		return
	}
	data.allResponsesHTML += updatedResponsesBytes.String()
	data.allResponsesHTML = strings.ReplaceAll(data.allResponsesHTML, "\n", "")
	data.sendClientsResponseHTML()
}
