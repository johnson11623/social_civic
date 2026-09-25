package sms

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAfricasTalkingSend(t *testing.T) {
	var got url.Values
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("apiKey")
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"SMSMessageData":{"Message":"Sent to 1/1","Recipients":[{"statusCode":101,"number":"+254712345678","status":"Success","cost":"KES 0.8000","messageId":"ATXid_1"}]}}`)
	}))
	defer srv.Close()

	s := &AfricasTalking{Username: "civic", APIKey: "k", SenderID: "CIVIC", Endpoint: srv.URL}
	if err := s.Send(context.Background(), "+254712345678", "Your code is 123456"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if key != "k" || got.Get("username") != "civic" || got.Get("to") != "+254712345678" ||
		got.Get("message") != "Your code is 123456" || got.Get("from") != "CIVIC" {
		t.Fatalf("request: key=%q form=%v", key, got)
	}
}

func TestAfricasTalkingRefusals(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"invalid number": {201, `{"SMSMessageData":{"Message":"Sent to 0/1","Recipients":[{"statusCode":403,"status":"InvalidPhoneNumber"}]}}`},
		"no recipients":  {201, `{"SMSMessageData":{"Message":"InvalidSenderId","Recipients":[]}}`},
		"bad key":        {401, `The supplied authentication is invalid`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			defer srv.Close()
			err := (&AfricasTalking{Username: "u", APIKey: "k", Endpoint: srv.URL}).Send(context.Background(), "+254700000000", "x")
			if err == nil {
				t.Fatal("want an error")
			}
			if c.status < 300 && !errors.Is(err, ErrRejected) {
				t.Fatalf("want ErrRejected, got %v", err)
			}
		})
	}
}

func TestSandboxEndpoint(t *testing.T) {
	if (&AfricasTalking{Username: "sandbox"}).endpoint() != atSandboxURL ||
		(&AfricasTalking{Username: "Fairtrade"}).endpoint() != atLiveURL {
		t.Fatal("wrong endpoint")
	}
}
