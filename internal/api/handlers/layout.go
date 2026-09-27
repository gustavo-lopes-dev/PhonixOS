package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
)

// Layout vincula a configuração espacial dos cards ao Store do servidor.
type Layout struct{ Store *database.Store }

// A migration inicial define a grade canônica do dashboard com seis colunas.
const dashboardGridColumns = 6

func (h Layout) Get(c *fiber.Ctx) error {
	cards, err := h.Store.ListLayoutCards(c.UserContext())
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, cards)
}

func (h Layout) Put(c *fiber.Ctx) error {
	var body struct {
		Cards []database.UpdateLayoutCardDTO `json:"cards"`
	}
	if err := parseBody(c, &body); err != nil {
		return err
	}
	if body.Cards == nil {
		return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "O campo cards é obrigatório.")
	}
	var raw struct {
		Cards []map[string]json.RawMessage `json:"cards"`
	}
	if err := json.Unmarshal(c.Body(), &raw); err != nil {
		return api.InvalidJSONError{}
	}
	seen := make(map[string]bool, len(body.Cards))
	for i, card := range body.Cards {
		for _, name := range [...]string{"card_key", "pos_x", "pos_y", "width", "height", "is_visible"} {
			if _, present := raw.Cards[i][name]; !present {
				return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Todos os campos do card são obrigatórios.")
			}
		}
		for _, value := range raw.Cards[i] {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Dados de layout inválidos.")
			}
		}
		if strings.TrimSpace(card.CardKey) == "" || card.PosX < 0 || card.PosY < 0 || card.Width <= 0 || card.Height <= 0 || seen[card.CardKey] ||
			card.PosX >= dashboardGridColumns || card.Width > dashboardGridColumns-card.PosX {
			return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Dados de layout inválidos.")
		}
		seen[card.CardKey] = true
	}
	current, err := h.Store.ListLayoutCards(c.UserContext())
	if err != nil {
		return databaseFailure(c, err)
	}
	byKey := make(map[string]database.LayoutCard, len(current))
	for _, card := range current {
		byKey[card.CardKey] = card
	}
	for _, card := range body.Cards {
		if existing, ok := byKey[card.CardKey]; ok && (card.Width < existing.MinWidth || card.Height < existing.MinHeight) {
			return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Dimensões menores que o mínimo do card.")
		}
	}
	cards, err := h.Store.UpdateLayoutCards(c.UserContext(), body.Cards)
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, cards)
}
