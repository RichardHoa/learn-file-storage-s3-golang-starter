package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {

	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}

	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "You are not the owner of this video: ", nil)
		return
	}

	file, header, err := r.FormFile("video")
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

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type, you can only upload image", nil)
		return
	}

	tempF, err := os.CreateTemp("", "tubely-upload-video-mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer tempF.Close()
	defer os.Remove(tempF.Name())

	_, err = io.Copy(tempF, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to write to temp file", err)
		return
	}

	optimizedFilePath, err := processVideoForFastStart(tempF.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to optimize video", err)
		return
	}

	optimizedF, err := os.Open(optimizedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open optimized video", err)
		return
	}
	defer optimizedF.Close()

	aspectRatio, err := getVideoAspectRatio(tempF.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video aspect ratio", err)
		return
	}

	switch aspectRatio {
	case "16:9":
		aspectRatio = "landscape"
	case "9:16":
		aspectRatio = "portrait"
	default:
		aspectRatio = "other"
	}

	_, err = tempF.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to return temp file pointer to the beginning", err)
		return
	}

	key := aspectRatio + "/" + videoIDString + ".mp4"

	_, err = cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &key,
		Body:        optimizedF,
		ContentType: &mediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video", err)
		return
	}

	videoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, key)

	video.VideoURL = &videoURL


	video, err = cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video url", err)
		return
	}

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video url", err)
		return
	}
}

func getVideoAspectRatio(filePath string) (string, error) {
	var data bytes.Buffer

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmd.Stdout = &data

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var output struct {
		Streams []struct {
			DisplayAspectRatio string `json:"display_aspect_ratio"`
		} `json:"streams"`
	}

	err = json.Unmarshal(data.Bytes(), &output)

	aspectRatio := output.Streams[0].DisplayAspectRatio

	return aspectRatio, nil
}

func processVideoForFastStart(filePath string) (string, error) {

	outputPath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i",
		filePath, "-c", "copy", "-movflags",
		"faststart", "-f", "mp4", outputPath)

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return outputPath, nil
}


