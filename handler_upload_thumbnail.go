package main

import (
	"fmt"
	"io"
	"net/http"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	//"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)
	f, fHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer f.Close()
	mediaType := fHeader.Header.Get("Content-Type")
	imageData, err := io.ReadAll(f)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to read form file", err)
		return
	}

	vidMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video from database", err)
		return
	}
	if userID != vidMeta.UserID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user for video", err)
	}

	newThumbnail := thumbnail{
		data:      imageData,
		mediaType: mediaType,
	}

	videoThumbnails[videoID] = newThumbnail

	newURL := fmt.Sprintf("http://localhost:8091/api/thumbnails/%v", videoID)

	vidMeta.ThumbnailURL = &newURL
	err = cfg.db.UpdateVideo(vidMeta)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video metadata in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vidMeta)
}
