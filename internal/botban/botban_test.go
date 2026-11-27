package botban

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBanSendsTokenAndParsesResult(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 200)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Write([]byte(`{"result":"banned"}`))
	}))
	defer srv.Close()

	res, err := New(srv.URL, "tok").Ban(context.Background(), 42, "geo")
	if err != nil || res != "banned" {
		t.Fatalf("res=%q err=%v", res, err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth=%q", gotAuth)
	}
	if gotBody != `{"reason":"geo","telegram_id":42}` {
		t.Errorf("body=%q", gotBody)
	}
}

func TestBanErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"401":      func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) },
		"500":      func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusInternalServerError) },
		"bad json": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("zzz")) },
		"empty":    func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) },
	} {
		srv := httptest.NewServer(h)
		if res, err := New(srv.URL, "t").Ban(context.Background(), 1, "geo"); err == nil {
			t.Errorf("%s: ожидалась ошибка, получено %q", name, res)
		}
		srv.Close()
	}
	if _, err := New("http://127.0.0.1:1", "t").Ban(context.Background(), 1, "geo"); err == nil {
		t.Error("недоступный адрес должен дать ошибку")
	}
}
