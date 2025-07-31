package models

import (
	"testing"

	"github.com/tigrisdata-community/tygen/models/modelstest"
)

func TestNewDAO(t *testing.T) {
	dbURL := modelstest.MaybeSpawnDB(t)

	dao, err := New(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = dao
}
