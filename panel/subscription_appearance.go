package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ildarmaga/wdtt/pkg/paneldb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type subAppearance struct {
	Color       string `json:"color"`
	Radius      int    `json:"radius"`
	Theme       string `json:"theme"`
	Logo        string `json:"logo"`
	Background  string `json:"background"`
	Surface     string `json:"surface"`
	Text        string `json:"text"`
	Heading     string `json:"heading"`
	Instruction string `json:"instruction"`
}

func validateSubAppearance(v *subAppearance) error {
	if !subPageColorPattern.MatchString(v.Color) {
		return fmt.Errorf("invalid accent color")
	}
	for _, color := range []string{v.Background, v.Surface, v.Text} {
		if color != "" && !subPageColorPattern.MatchString(color) {
			return fmt.Errorf("invalid color")
		}
	}
	if v.Radius < 0 || v.Radius > 32 || (v.Theme != "auto" && v.Theme != "dark" && v.Theme != "light") {
		return fmt.Errorf("invalid theme or radius")
	}
	if v.Logo != "" {
		u, err := url.Parse(v.Logo)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("logo must be an HTTPS URL")
		}
	}
	if len(v.Heading) > 200 || len(v.Instruction) > 8000 || len(v.Logo) > 2048 {
		return fmt.Errorf("text is too long")
	}
	return nil
}

func readSubAppearance() (map[string]interface{}, error) {
	defaults := subscriptionPageAppearance()
	var raw string
	if m, err := paneldb.GetDefaultMongo(); err == nil && m != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var doc struct {
			Value string `bson:"value"`
		}
		err = m.Database().Collection("panel_settings").FindOne(ctx, bson.M{"_id": "subscription_appearance"}).Decode(&doc)
		if err == mongo.ErrNoDocuments {
			return defaults, nil
		}
		if err != nil {
			return nil, err
		}
		raw = doc.Value
	} else if panelDBEnabled() {
		err := panelDB.QueryRow(`SELECT value FROM settings WHERE key = ?`, "subscription_appearance").Scan(&raw)
		if err == sql.ErrNoRows {
			return defaults, nil
		}
		if err != nil {
			return nil, err
		}
	} else {
		return defaults, nil
	}
	var saved subAppearance
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return nil, err
	}
	if err := validateSubAppearance(&saved); err != nil {
		return nil, err
	}
	var result map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &result)
	return result, nil
}

func loadSubscriptionAppearance() map[string]interface{} {
	v, err := readSubAppearance()
	if err != nil {
		return subscriptionPageAppearance()
	}
	return v
}

func (a *App) handleSubAppearance(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		v, err := readSubAppearance()
		if err != nil {
			jsonError(w, err.Error(), 503)
			return
		}
		jsonOK(w, v)
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	var v subAppearance
	if err := readJSON(r, &v); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	v.Logo = strings.TrimSpace(v.Logo)
	v.Heading = strings.TrimSpace(v.Heading)
	v.Instruction = strings.TrimSpace(v.Instruction)
	if err := validateSubAppearance(&v); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	if m, e := paneldb.GetDefaultMongo(); e == nil && m != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_, err = m.Database().Collection("panel_settings").UpdateOne(ctx, bson.M{"_id": "subscription_appearance"}, bson.M{"$set": bson.M{"value": string(raw)}}, options.UpdateOne().SetUpsert(true))
	} else if panelDBEnabled() {
		_, err = panelDB.ExecContext(r.Context(), `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "subscription_appearance", string(raw))
	} else {
		err = fmt.Errorf("database unavailable")
	}
	if err != nil {
		jsonError(w, err.Error(), 503)
		return
	}
	jsonOK(w, v)
}
