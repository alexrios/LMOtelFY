package samples

import (
	"context"
	"errors"
	"log"
	"net/http"
)

func H(w http.ResponseWriter, r *http.Request) {
	err := errors.New("new error")
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func C(ctx context.Context) error {
	err := errors.New("new error")
	if err != nil {
		return err
	}

	return nil
}
