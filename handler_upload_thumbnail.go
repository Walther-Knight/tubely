package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"

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
	mediaType, _, err := mime.ParseMediaType(fHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse media type from file", err)
		return
	}

	if mediaType != "image/jpg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Incorrect media type for thumbnail", nil)
		return
	}

	key := make([]byte, 32)
	rand.Read(key)
	randPath := base64.RawURLEncoding.EncodeToString(key)

	fileType := filepath.Ext(fHeader.Filename)
	filePath := filepath.Join(cfg.assetsRoot, randPath+fileType)
	dataTarget, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to create file", err)
		return
	}
	defer dataTarget.Close()

	io.Copy(dataTarget, f)

	vidMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video from database", err)
		return
	}
	if userID != vidMeta.UserID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user for video", err)
		return
	}

	//support for global map
	//newThumbnail := thumbnail{
	//	data:      imageData,
	//	mediaType: mediaType,
	//}

	//videoThumbnails[videoID] = newThumbnail

	newURL := fmt.Sprintf("http://localhost:8091/assets/%v%v", randPath, fileType)

	vidMeta.ThumbnailURL = &newURL
	err = cfg.db.UpdateVideo(vidMeta)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video metadata in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vidMeta)
}
