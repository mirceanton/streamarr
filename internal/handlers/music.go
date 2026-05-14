package handlers

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
)

func MusicHandler(w http.ResponseWriter, r *http.Request) {
	needsAttention := r.URL.Query().Get("attention") == "1"

	roots, err := db.GetLibraryRoots()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Collect music library roots
	var musicRoots []models.LibraryRoot
	for _, root := range roots {
		if root.Type == "music" {
			musicRoots = append(musicRoots, root)
		}
	}

	// Collect albums from all music libraries
	albumMap := make(map[string]*models.Album) // key: libraryRootID + artist + album
	for _, root := range musicRoots {
		albums, err := db.GetAlbums(root.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for i := range albums {
			if needsAttention && !albums[i].NeedsAttention {
				continue
			}
			key := strconv.FormatInt(root.ID, 10) + "|" + albums[i].Artist + "|" + albums[i].Title
			albumMap[key] = &albums[i]
		}
	}

	var albums []models.Album
	for _, a := range albumMap {
		albums = append(albums, *a)
	}
	sort.Slice(albums, func(i, j int) bool {
		if albums[i].Artist != albums[j].Artist {
			return albums[i].Artist < albums[j].Artist
		}
		return albums[i].Title < albums[j].Title
	})

	globalAudioFormat, _ := db.GetPreferredAudioFormat()

	data := map[string]interface{}{
		"Page":              "music",
		"Albums":            albums,
		"NeedsAttention":    needsAttention,
		"GlobalAudioFormat": globalAudioFormat,
	}
	render(w, "music.html", data)
}

func AlbumTracksHandler(w http.ResponseWriter, r *http.Request) {
	// albumKey is URL-encoded "artist/album"
	albumKey, err := url.PathUnescape(chi.URLParam(r, "albumKey"))
	if err != nil {
		http.Error(w, "Invalid album key", http.StatusBadRequest)
		return
	}

	libRootIDStr := r.URL.Query().Get("library_root_id")
	libRootID, err := strconv.ParseInt(libRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid library_root_id", http.StatusBadRequest)
		return
	}

	parts := strings.SplitN(albumKey, "/", 2)
	artist := ""
	albumTitle := albumKey
	if len(parts) == 2 {
		artist = parts[0]
		albumTitle = parts[1]
	}

	tracks, err := db.GetTracksByAlbum(artist, albumTitle, libRootID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	audioFormatOverride, _ := db.GetAudioFormatOverride(libRootID, albumKey, "album")
	globalAudioFormat, _ := db.GetPreferredAudioFormat()
	globalMinBitrate, _ := db.GetPreferredMinBitrate()

	data := map[string]interface{}{
		"Page":                "music",
		"AlbumKey":            albumKey,
		"Artist":              artist,
		"AlbumTitle":          albumTitle,
		"LibraryRootID":       libRootID,
		"Tracks":              tracks,
		"AudioFormatOverride": audioFormatOverride,
		"GlobalAudioFormat":   globalAudioFormat,
		"GlobalMinBitrate":    globalMinBitrate,
	}
	render(w, "album.html", data)
}

func SetAlbumAudioFormatOverrideHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	albumKey := r.FormValue("album_key")
	libRootIDStr := r.FormValue("library_root_id")
	format := strings.TrimSpace(strings.ToLower(r.FormValue("audio_format")))

	libRootID, err := strconv.ParseInt(libRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid library_root_id", http.StatusBadRequest)
		return
	}

	if !validAudioFormats[format] {
		http.Error(w, "Invalid audio format", http.StatusBadRequest)
		return
	}

	if format == "" {
		if err := db.DeleteAudioFormatOverride(libRootID, albumKey, "album"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := db.SetAudioFormatOverride(libRootID, albumKey, "album", format); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	redirectURL := "/music/" + url.PathEscape(albumKey) + "?library_root_id=" + libRootIDStr
	w.Header().Set("HX-Redirect", redirectURL)
	w.WriteHeader(http.StatusOK)
}

func DeleteAlbumAudioFormatOverridePostHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	albumKey := r.FormValue("album_key")
	libRootIDStr := r.FormValue("library_root_id")

	libRootID, err := strconv.ParseInt(libRootIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid library_root_id", http.StatusBadRequest)
		return
	}

	if err := db.DeleteAudioFormatOverride(libRootID, albumKey, "album"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectURL := "/music/" + url.PathEscape(albumKey) + "?library_root_id=" + libRootIDStr
	w.Header().Set("HX-Redirect", redirectURL)
	w.WriteHeader(http.StatusOK)
}
