package game

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog/log"
)

var (
	gameTemplate = template.Must(template.ParseFiles("templates/gameBase.html", "templates/gameItem.html"))
)

type gameStateEnum int

const (
	gameState_AwaitingQuestion gameStateEnum = iota
	gameState_AwaitingAnswer   gameStateEnum = iota
)

// --------------------------------------------------------------------------------
// JWT Data and Methods
// --------------------------------------------------------------------------------

type oracleJWTData struct {
	key         []byte
	tokenString string
}

func (data *gameData) checkRequestFromOracle(r *http.Request) bool {
	// Ensure cookie actually exits

	tokenCookie, err := r.Cookie("oracleToken-" + data.gameID)
	if err != nil {
		if err == http.ErrNoCookie {
			log.Debug().Msg("Oracle JWT Check - No Token Cookie")
		}
		log.Debug().Msg("Oracle JWT Check - Error Reading Cookie")
		return false
	}

	// Get claims from cookie by decoding with key

	tokenString := tokenCookie.Value
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		return data.oracleJWT.key, nil
	})

	// log.Debug().Interface("JWT Claims", claims).Msg("Oracle JWT Check")

	// Ensure decoding did not fail

	if err != nil {
		if err == jwt.ErrSignatureInvalid {
			log.Debug().Msg("Oracle JWT Check - Invalid Signature")
		}
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

func (data *gameData) checkRequestFromOracleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), "IsOracle", data.checkRequestFromOracle(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --------------------------------------------------------------------------------
// Game Data struct
// --------------------------------------------------------------------------------

type questionAnswerPair struct {
	Index    int
	Question string
	Answer   string
}

type gameData struct {
	gameID              string
	oracleJWT           oracleJWTData
	router              *chi.Mux
	questionAnswerPairs []questionAnswerPair
	gameState           gameStateEnum
}

func newGameData(gameID string, oracleJWT oracleJWTData) *gameData {
	data := &gameData{
		gameID:              gameID,
		oracleJWT:           oracleJWT,
		router:              chi.NewRouter(),
		questionAnswerPairs: make([]questionAnswerPair, 0),
		gameState:           gameState_AwaitingQuestion,
	}

	data.router.Use(data.checkRequestFromOracleMiddleware)
	data.router.Get("/"+data.gameID+"/", data.getGameBaseTemplate)
	data.router.Post("/"+data.gameID+"/response", data.handleResponse)
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
	GameID string
}

func (data *gameData) getGameBaseTemplate(w http.ResponseWriter, r *http.Request) {
	err := gameBaseTemplate.Execute(w, gameBaseTemplateData{
		GameID: data.GameID,
	})
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game base template")
	}
}

type gameItemTemplateData struct {
	GameID     string
	ItemNumber int
}

func (data *gameData) renderNextItem(w http.ResponseWriter, r *http.Request) {
	data.numResponses += 1
	itemTemplateData := gameItemTemplateData{
		GameID:     data.GameID,
		ItemNumber: data.numResponses,
	}

	err := gameItemTemplate.Execute(w, itemTemplateData)
	if err != nil {
		log.Error().Interface("GameData", data).Err(err).Msg("Failed to write game item template")
	}
}
