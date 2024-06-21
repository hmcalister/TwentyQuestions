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
