package parentportal

import (
	"embed"
	"net/http"
)

//go:embed web/*
var web embed.FS

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/hls.min.js" {
		http.ServeFile(w, r, s.cfg.HLSJS)
		return
	}
	name, contentType := "", ""
	switch r.URL.Path {
	case "/":
		name = "index.html"
		contentType = "text/html; charset=utf-8"
	case "/code":
		name = "code.html"
		contentType = "text/html; charset=utf-8"
	case "/code.js":
		name = "code.js"
		contentType = "text/javascript; charset=utf-8"
	case "/app.js":
		name = "app.js"
		contentType = "text/javascript; charset=utf-8"
	case "/style.css":
		name = "style.css"
		contentType = "text/css; charset=utf-8"
	case "/orchids.webp":
		name = "orchids.webp"
		contentType = "image/webp"
	case "/orion.png":
		name = "orion.png"
		contentType = "image/png"
	case "/geist.woff2":
		name = "geist.woff2"
		contentType = "font/woff2"
	default:
		fail(w, 404, "Not found.")
		return
	}
	b, err := web.ReadFile("web/" + name)
	if err != nil {
		fail(w, 500, "Page unavailable.")
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(b)
}
