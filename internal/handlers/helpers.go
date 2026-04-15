package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/mirceanton/streamarr/internal/scanner"
)

var pageTemplates map[string]*template.Template

var funcMap = template.FuncMap{
	"codecName":     scanner.CodecDisplayName,
	"channelLayout": scanner.ChannelLayout,
	"isImageSub":    scanner.IsImageBasedSubtitle,
	"subExt":        scanner.SubtitleExtension,
	"langName":      languageName,
	"formatSize":    formatSize,
	"upper":         strings.ToUpper,
	"add":           func(a, b int) int { return a + b },
	"deref":         func(p *int) int { if p != nil { return *p }; return 0 },
	"hasValue":      func(p *int) bool { return p != nil },
}

func InitTemplates() error {
	pageTemplates = make(map[string]*template.Template)

	pages := []string{
		"movies.html",
		"shows.html",
		"media_detail.html",
		"settings.html",
		"jobs.html",
	}

	for _, page := range pages {
		t, err := template.New("").Funcs(funcMap).ParseFiles("templates/base.html", "templates/"+page)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", page, err)
		}
		pageTemplates[page] = t
	}

	return nil
}

func render(w http.ResponseWriter, name string, data interface{}) {
	t, ok := pageTemplates[name]
	if !ok {
		log.Printf("template %s not found", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("render template %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func renderFragment(w http.ResponseWriter, name string, data interface{}) {
	// Fragments are defined across page templates, search all
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, t := range pageTemplates {
		if tmpl := t.Lookup(name); tmpl != nil {
			if err := tmpl.Execute(w, data); err != nil {
				log.Printf("render fragment %s: %v", name, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}
	}
	log.Printf("fragment template %s not found", name)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func languageName(code string) string {
	languages := map[string]string{
		"eng": "English",
		"fra": "French",
		"fre": "French",
		"deu": "German",
		"ger": "German",
		"spa": "Spanish",
		"ita": "Italian",
		"por": "Portuguese",
		"rus": "Russian",
		"jpn": "Japanese",
		"kor": "Korean",
		"zho": "Chinese",
		"chi": "Chinese",
		"ara": "Arabic",
		"hin": "Hindi",
		"tur": "Turkish",
		"pol": "Polish",
		"nld": "Dutch",
		"dut": "Dutch",
		"swe": "Swedish",
		"nor": "Norwegian",
		"dan": "Danish",
		"fin": "Finnish",
		"ces": "Czech",
		"cze": "Czech",
		"hun": "Hungarian",
		"ron": "Romanian",
		"rum": "Romanian",
		"bul": "Bulgarian",
		"hrv": "Croatian",
		"srp": "Serbian",
		"slv": "Slovenian",
		"ukr": "Ukrainian",
		"ell": "Greek",
		"gre": "Greek",
		"heb": "Hebrew",
		"tha": "Thai",
		"vie": "Vietnamese",
		"ind": "Indonesian",
		"msa": "Malay",
		"may": "Malay",
		"cat": "Catalan",
		"eus": "Basque",
		"baq": "Basque",
		"glg": "Galician",
		"lat": "Latin",
		"und": "Undefined",
		"":    "Unknown",
	}
	if name, ok := languages[strings.ToLower(code)]; ok {
		return name
	}
	return strings.ToUpper(code)
}
