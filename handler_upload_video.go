package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading video file", videoID, "by user", userID)

	const maxMemory = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)
	r.ParseMultipartForm(maxMemory)
	f, fHeader, err := r.FormFile("video")
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

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Incorrect media type for video upload", nil)
		return
	}

	vidMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video from database", err)
		return
	}
	if userID != vidMeta.UserID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized user for video", err)
		return
	}

	key := make([]byte, 32)
	rand.Read(key)
	randPath := hex.EncodeToString(key)

	fileExt := filepath.Ext(fHeader.Filename)
	fileName := randPath + fileExt

	vidTemp, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer os.Remove(vidTemp.Name())
	defer vidTemp.Close()

	io.Copy(vidTemp, f)
	prefix, err := getVideoAspectRatio(vidTemp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error parsing temp file for aspect ratio", err)
		return
	}

	if prefix == "" {
		return
	}

	fileName = prefix + "/" + fileName
	vidTemp.Seek(0, io.SeekStart)

	procPath, err := processVideoForFastStart(vidTemp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error processing temp file for fast start", err)
		return
	}
	procVid, err := os.Open(procPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening temp file for fast start", err)
		return
	}
	defer os.Remove(procPath)
	defer procVid.Close()

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileName,
		ContentType: &mediaType,
		Body:        procVid,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to PutObject in S3", err)
		return
	}

	newURL := fmt.Sprintf("https://d18j4f4pnun7t0.cloudfront.net/%v", fileName)

	vidMeta.VideoURL = &newURL

	err = cfg.db.UpdateVideo(vidMeta)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video metadata in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, vidMeta)
}

func getVideoAspectRatio(filePath string) (string, error) {
	var bufCmd bytes.Buffer
	cmdRes := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmdRes.Stdout = &bufCmd
	err := cmdRes.Run()
	if err != nil {
		log.Println(err)
		return "", err
	}

	type SizeCalc struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	bufContent := bufCmd.Bytes()

	var ResJson SizeCalc
	err = json.Unmarshal(bufContent, &ResJson)
	if err != nil {

		log.Printf("Error unmarshalling JSON in video aspect: %v", err)
		return "", nil
	}

	if ResJson.Streams[0].Width/ResJson.Streams[0].Height == 1 {
		return "landscape", nil
	}

	if ResJson.Streams[0].Width/ResJson.Streams[0].Height == 0 {
		return "portrait", nil
	}

	return "other", nil
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"
	cmdRes := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	err := cmdRes.Run()
	if err != nil {
		log.Println(err)
		return "", err
	}
	return outputPath, nil
}
