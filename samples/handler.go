package samples

import (
	"context"
	"net/http"
)

func H(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func C(ctx context.Context) error {
	return nil
}
