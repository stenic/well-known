package main

import (
	"context"
	"encoding/json"
	"net/http"
)

type WellKnownGetter interface {
	GetData(ctx context.Context) (*wkRegistry, error)
}

func GetHealthServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	return mux
}

func GetServer(wks WellKnownGetter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/{id}", func(w http.ResponseWriter, r *http.Request) {
		reg, err := wks.GetData(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Failed to fetch well-known records"))
			return
		}

		if reg == nil {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not found"))
			return
		}

		items := reg
		id := r.PathValue("id")
		if val, ok := (*items)[id]; ok {
			b, err := json.Marshal(val)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Failed to encode"))
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Write(b)
			return
		}

		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
	})

	return mux
}
