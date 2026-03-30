package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
)

type ScoreEntry struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

const leaderboardFile = "leaderboard.json"

var mu sync.Mutex

func loadLeaderboard() []ScoreEntry {
	data, err := os.ReadFile(leaderboardFile)
	if err != nil {
		return nil
	}
	var entries []ScoreEntry
	json.Unmarshal(data, &entries)
	return entries
}

func saveLeaderboard(entries []ScoreEntry) {
	data, _ := json.MarshalIndent(entries, "", "  ")
	os.WriteFile(leaderboardFile, data, 0644)
}

func main() {
	fs := http.FileServer(http.Dir("."))

	http.HandleFunc("/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		mu.Lock()
		defer mu.Unlock()

		if r.Method == http.MethodPost {
			var entry ScoreEntry
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil || entry.Name == "" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if len(entry.Name) > 8 {
				entry.Name = entry.Name[:8]
			}
			entries := loadLeaderboard()
			entries = append(entries, entry)
			sort.Slice(entries, func(i, j int) bool { return entries[i].Level > entries[j].Level })
			if len(entries) > 10 {
				entries = entries[:10]
			}
			saveLeaderboard(entries)
			json.NewEncoder(w).Encode(entries)
		} else {
			entries := loadLeaderboard()
			if entries == nil {
				entries = []ScoreEntry{}
			}
			json.NewEncoder(w).Encode(entries)
		}
	})

	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".wasm") {
			w.Header().Set("Content-Type", "application/wasm")
		}
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		fs.ServeHTTP(w, r)
	}))

	log.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
