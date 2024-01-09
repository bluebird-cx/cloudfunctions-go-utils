package cloudfunctions_go_utils

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
)

var (
	// LogTypeError1 - error log level with higher priority (than LogTypeError2)
	LogTypeError1 string = "error1"
	// LogTypeError2 - error log level with lower priority (than LogTypeError1)
	LogTypeError2 string = "error2"
	// LogTypeInfo - informational log level
	LogTypeInfo string = "info"

	// ErrorCodeExternalAPI - external API ErrorCode
	ErrorCodeExternalAPI int = 100
	// ErrorCodeInternal - internal API ErrorCode
	ErrorCodeInternal int = 500
	// ErrorCodeFirebase - Firebase API ErrorCode
	ErrorCodeFirebase int = 510
)

// LogWrite function to be called for all logs to be able to parse logs in the right format
// user ID is optional since may be unavailable at some points, ex: parsing request
func LogWrite(logType string, errorCode int, errorMessage string, userId string) {
	var userIdMessagePart string
	if userId != "" {
		userIdMessagePart = fmt.Sprintf(", UserId: %v", userId)
	}
	log.Printf("application:server, " + "logType:" + logType + ", errorCode:" + strconv.Itoa(errorCode) + userIdMessagePart + ", message:" + errorMessage)
}

// LogWriteDebug - function used for logging some extra data needed for debugging
// it works only in "DEBUG" env variable was set to true in deploy instruction
func LogWriteDebug(message string) {
	if os.Getenv("DEBUG") == strconv.FormatBool(true) {
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("[DEBUG] %v", message), "")
	}
}

// WriteHTTPError allows you to create an error http with json as a response,
// and "message" as the map key
func WriteHTTPError(w http.ResponseWriter, message string, statusCode int) {
	bodyMap := map[string]string{
		"message": message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(bodyMap)
}

func setCORSHeaders(w *http.ResponseWriter, allowMethods string) {

	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", allowMethods)
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, Accept, Authorization, auth_code, redirect_url, Type, Version, crm_type, email_provider")
	(*w).Header().Set("Access-Control-Max-Age", "3600")
	(*w).WriteHeader(http.StatusNoContent)
}

func getURLParameter(r *http.Request, queryKey string, defaultValue string) string {
	paramSlice, OK := r.URL.Query()[queryKey]
	var param string
	if !OK || len(paramSlice[0]) < 1 || paramSlice[0] == "" {
		param = defaultValue
	} else {
		param = paramSlice[0]
	}

	return param
}
