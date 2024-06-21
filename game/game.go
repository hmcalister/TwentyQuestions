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
	"github.com/rs/zerolog/log"
)

var (
	// Templates for Game page.
	gameTemplate = template.Must(template.ParseFiles("templates/gameBase.html", "templates/gameItem.html"))
)

// Enum for gameState, determining what is required next.
type gameStateEnum int

const (
	gameState_AwaitingQuestion gameStateEnum = iota
	gameState_AwaitingAnswer   gameStateEnum = iota
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

	// Array of all sseClients -- pruned of closed clients when next event is sent.
	sseClients []*sseClient

	// Mutex to ensure atomic handling of SSE clients -- we don't want to accidentally miss a client!
	sseClientsMutex sync.Mutex
}

// Create a new game data, including registering routes on router.
func newGameData(gameID string, oracleJWTKey []byte) *GameData {
	data := &GameData{
		gameID:              gameID,
		oracleJWTKey:        oracleJWTKey,
		router:              chi.NewRouter(),
		gameState:           gameState_AwaitingQuestion,
		questionAnswerPairs: make([]questionAnswerPair, 0),
		allResponsesHTML:    "",
		sseClients:          make([]*sseClient, 0),
	}

	// Always check if request is from the oracle.
	data.router.Use(data.checkRequestFromOracleMiddleware)

	data.router.Get("/"+data.gameID+"/", data.renderGameBase)
	data.router.Post("/"+data.gameID+"/submitResponse", data.handleResponse)
	data.router.Get("/"+data.gameID+"/responsesSourceSSE", data.responsesSourceSSE)

	return data
}

func (data *gameData) addNextQuestion(question string) error {
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

func (data *gameData) addNextAnswer(answer string) error {
	if data.gameState != gameState_AwaitingAnswer {
		return errors.New("not currently awaiting answer")
	}

	data.questionAnswerPairs[len(data.questionAnswerPairs)-1].Answer = answer
	data.gameState = gameState_AwaitingQuestion
	return nil
}

// --------------------------------------------------------------------------------
// Routing Functions
// --------------------------------------------------------------------------------

type gameBaseTemplateData struct {
	GameID              string
	IsOracle            bool
	QuestionAnswerPairs []questionAnswerPair
}

func (data *gameData) getGameBaseTemplate(w http.ResponseWriter, r *http.Request) {
	err := gameTemplate.ExecuteTemplate(w, "gameBase.html", gameBaseTemplateData{
		GameID:              data.gameID,
		IsOracle:            r.Context().Value("IsOracle").(bool),
		QuestionAnswerPairs: data.questionAnswerPairs,
	})
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game base template")
	}
}

func (data *gameData) handleResponse(w http.ResponseWriter, r *http.Request) {
	response := r.FormValue("response")
	log.Debug().Str("Game ID", data.gameID).Str("Response", response).Msg("Game Response")

	if len(response) == 0 {
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

	var updatedResponsesBytes bytes.Buffer
	err := gameTemplate.ExecuteTemplate(&updatedResponsesBytes, "gameItem.html", gameBaseTemplateData{QuestionAnswerPairs: data.questionAnswerPairs})
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game item template")
		return
	}
	data.allResponsesHTML = updatedResponsesBytes.String()
	data.allResponsesHTML = strings.ReplaceAll(data.allResponsesHTML, "\n", "")

	for i := 0; i < len(data.sseClients); {
		currentClient := data.sseClients[i]
		select {
		case <-currentClient.context.Done():
			close(currentClient.responsesChannel)
			data.sseClients[i] = data.sseClients[len(data.sseClients)-1]
			data.sseClients = data.sseClients[:len(data.sseClients)-1]
		default:
			currentClient.responsesChannel <- data.allResponsesHTML
			i += 1
		}
	}
}

func (data *gameData) responsesSourceSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	responsesChannel := make(chan string)
	data.sseClients = append(data.sseClients, &sseClient{
		context:          r.Context(),
		responsesChannel: responsesChannel,
	})

	// Send data to the client
	go func() {
		for responsesHTML := range responsesChannel {
			fmt.Fprintf(w, "data: %s\n\n", responsesHTML)
			w.(http.Flusher).Flush()
		}
	}()

	<-r.Context().Done()
}
