package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"path/filepath"

	"mime"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
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

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	// Get content-type of the img file
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse media type", err)
		return
	}

	if !strings.HasPrefix(mediaType, "image/") {
		respondWithError(w, http.StatusBadRequest, "Invalid media type, you can only upload image", nil)
		return
	}

	// Get the video from the database
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}

	// Check if the user is the owner
	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of this video: ", nil)
		return
	}

	// Save the img file to assets folder
	imgType := strings.TrimPrefix(mediaType, "image/")

	sliceByte := make([]byte, 32)

	_, err = rand.Read(sliceByte)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate random bytes", err)
		return
	}

	base64String := base64.RawURLEncoding.EncodeToString(sliceByte)

	imgName := fmt.Sprintf("%s.%s", base64String, imgType)

	finalPath := filepath.Join(cfg.assetsRoot, imgName)

	imgFile, err := os.Create(finalPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create file", err)
		return
	}
	defer imgFile.Close()

	_, err = io.Copy(imgFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy file", err)
		return
	}

	dataURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, imgName)

	// update the video thumbnail
	video.ThumbnailURL = &dataURL

	// Update video to the database
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}




