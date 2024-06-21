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

type gameData struct {
	GameID       string
	router       *chi.Mux
	numResponses int
}

func newGameData(gameID string) *gameData {
	data := &gameData{
		GameID:       gameID,
		router:       chi.NewRouter(),
		numResponses: 0,
	}

	// data.router.Get("/"+data.GameID+"/*", data.testRoute)
	data.router.Get("/"+data.GameID+"/", data.getGameBaseTemplate)
	data.router.Post("/"+data.GameID+"/add", data.renderNextItem)

	return data
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
