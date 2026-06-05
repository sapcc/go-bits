// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package respondwith_test

import (
	"errors"
	"net/http"
	"regexp"
	"testing"

	"go.xyrillian.de/gg/jsonmatch"

	"github.com/sapcc/go-bits/assert"
	"github.com/sapcc/go-bits/httptest"
	"github.com/sapcc/go-bits/respondwith"
)

func TestCustomStatus(t *testing.T) {
	ctx := t.Context()

	exampleHandler := func(r *http.Request) (map[string]any, error) {
		switch r.URL.Path {
		case "/ok":
			return map[string]any{"success": true}, nil
		case "/servererror":
			return nil, errors.New("datacenter on fire")
		case "/ratelimit":
			return nil, respondwith.CustomStatus(http.StatusTooManyRequests,
				errors.New("ratelimit exceeded"),
				respondwith.CustomHeader("Retry-After", "60"),
			)
		default:
			return nil, respondwith.CustomStatus(http.StatusNotFound, errors.New("not found"))
		}
	}

	plainHandler := httptest.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := exampleHandler(r)
		if respondwith.ErrorText(w, err) {
			return
		}
		respondwith.JSON(w, http.StatusOK, result)
	}))

	obfuscatedHandler := httptest.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, err := exampleHandler(r)
		if respondwith.ObfuscatedErrorText(w, err) {
			return
		}
		respondwith.JSON(w, http.StatusOK, result)
	}))

	plainHandler.RespondTo(ctx, "GET /ok").
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"success": true})
	obfuscatedHandler.RespondTo(ctx, "GET /ok").
		ExpectJSON(t, http.StatusOK, jsonmatch.Object{"success": true})

	plainHandler.RespondTo(ctx, "GET /servererror").
		ExpectText(t, http.StatusInternalServerError, "datacenter on fire\n")
	obfuscatedHandler.RespondTo(ctx, "GET /servererror").Expect(func(r httptest.Response) {
		r.ExpectStatus(t, http.StatusInternalServerError)
		assert.Equal(t, regexp.MustCompile(`^Internal Server Error \(ID = .*\)\n$`).MatchString(r.BodyString()), true)
	})

	plainHandler.RespondTo(ctx, "GET /notfound").
		ExpectText(t, http.StatusNotFound, "not found\n")
	obfuscatedHandler.RespondTo(ctx, "GET /notfound").
		ExpectText(t, http.StatusNotFound, "not found\n")

	plainHandler.RespondTo(ctx, "GET /ratelimit").
		ExpectHeader(t, "Retry-After", "60").
		ExpectText(t, http.StatusTooManyRequests, "ratelimit exceeded\n")
	obfuscatedHandler.RespondTo(ctx, "GET /ratelimit").
		ExpectHeader(t, "Retry-After", "60").
		ExpectText(t, http.StatusTooManyRequests, "ratelimit exceeded\n")
}
