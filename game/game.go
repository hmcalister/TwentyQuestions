package game

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

var (
	gameBaseTemplate = template.Must(template.New("gameBase.html").ParseFiles("templates/gameBase.html"))
	gameItemTemplate = template.Must(template.New("gameItem.html").ParseFiles("templates/gameItem.html"))
)

type gameData struct {
	GameID       string
	router       *chi.Mux
	numResponses int
}

	GameID string
}
