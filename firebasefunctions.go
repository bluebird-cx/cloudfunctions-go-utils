package cloudfunctions_go_utils

import (
	"cloud.google.com/go/firestore"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"context"
	"errors"
	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"
	"fmt"
	"google.golang.org/api/iterator"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	ClosingTransportError   = "Unavailable desc = transport is closing"                   // message which Firestore returns connection issue error
	UnavailableServiceError = "Unavailable desc = The service is temporarily unavailable" // connection error message which Firestore returns

)

var (
	UsersCollection      string = "users"
	PromoItemsCollection string = "promo_items"
	// each time new collection is added to firestore - add it to this list
	FirestoreCollectionNames = []string{
		UsersCollection, PromoItemsCollection,
	}
)

// GetFirestoreClient Get a new Firestore Client
func getFirestoreAppAndClient() (*firebase.App, *firestore.Client, context.Context) {

	ctx := context.Background()
	fireapp, err := firebase.NewApp(ctx, nil)
	if err != nil {
		LogWrite(LogTypeError2, ErrorCodeFirebase, fmt.Sprintf("Error getting fireapp: %v", err.Error()), "")
		panic(err)
	}

	fireclient, err := fireapp.Firestore(ctx)
	if err != nil {
		LogWrite(LogTypeError2, ErrorCodeFirebase, fmt.Sprintf("Error getting fireclient: %v", err.Error()), "")
		panic(err)
	}

	return fireapp, fireclient, ctx
}

// GetSecret returns a string and error for a secret using Google's Secret Manager
// It gets the latest version of the secret.
func GetSecret(ctx context.Context, keyName string) (string, error) {
	var err error
	var secretBytes []byte
	// retry getting secret 2 times
	for i := 0; i < 2; i++ {
		secretBytes, err = getSecretRaw(ctx, keyName)
		if err == nil {
			return string(secretBytes), nil
		}
	}

	return "", err
}

// getSecretRaw returns a bytes array and error for a secret using Google's Secret Manager
// It gets the latest version of the secret.
func getSecretRaw(ctx context.Context, keyName string) ([]byte, error) {

	name := "projects/" + os.Getenv("GCLOUD_PROJECT") + "/secrets/" + keyName + "/versions/latest"

	// Create the client.
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	// Build the request.
	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	}

	// Call the API.
	result, err := client.AccessSecretVersion(ctx, req)
	if err != nil {
		return nil, err
	}
	return result.Payload.Data, nil
}

func checkFirebaseUserAuthorized(ctx context.Context, fireapp *firebase.App, fireclient *firestore.Client, r *http.Request) (*auth.Token, int) {
	authHeader := r.Header.Get("Authorization")
	//LogWrite(LogTypeInfo,0,authHeader)

	//check if the header is empty
	if authHeader == "" {
		LogWrite(LogTypeInfo, 0, "empty auth header", "")
		return nil, http.StatusUnauthorized
	}

	//we expect header to be "Bearer [token]" else it will fail
	tokenSlice := strings.Fields(authHeader)
	if len(tokenSlice) != 2 || tokenSlice[0] != "Bearer" {
		return nil, http.StatusUnauthorized
	}
	userToken := tokenSlice[1]

	//any error here will return as internal
	authClient, err := fireapp.Auth(ctx)
	if err != nil {
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("fireapp.Auth error: %v", err.Error()), "")
		return nil, http.StatusInternalServerError
	}

	token, err := authClient.VerifyIDToken(ctx, userToken)
	if err != nil || token == nil {
		//token failed
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("authClient.VerifyIDTokenError: %v", err.Error()), "")
		return nil, 401
	}

	return token, http.StatusOK
}

// firebaseDocumentIteratorWithRetry - gets firestore iterator with retries
func firebaseDocumentIteratorWithRetry(iter *firestore.DocumentIterator) (*firestore.DocumentSnapshot, error) {
	firestoreRetriesNumber, err := strconv.Atoi(os.Getenv("FIRESTORE_RETRIES_NUMBER"))
	if err != nil {
		firestoreRetriesNumber = 1
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("FIRESTORE_RETRIES_NUMBER is missing, was set to: %v", firestoreRetriesNumber), "")
	}

	for i := 0; i < firestoreRetriesNumber; i++ {
		doc, err := iter.Next()
		if err == nil {
			return doc, nil
		}

		if err.Error() == ClosingTransportError {
			//do retry if ClosingTransportError
			continue
		}

		// return iterator.Done to handle it in the right way
		if err == iterator.Done {
			return nil, err
		}

		//return if another error
		return nil, fmt.Errorf("Unsuccessful document iteration, Error: %v", err.Error())
	}

	// we got here if we exceed retries number
	return nil, fmt.Errorf("Unsuccessful document iteration, Error: %v", err.Error())
}

// addEntityToFirestore - adds any entity to the firestore collection with retries
func addEntityToFirestore(ctx context.Context, fireclient *firestore.Client, collectionName string, entity interface{}) (*firestore.DocumentRef, error) {
	var docRef *firestore.DocumentRef

	firestoreRetriesNumber, err := strconv.Atoi(os.Getenv("FIRESTORE_RETRIES_NUMBER"))
	if err != nil {
		firestoreRetriesNumber = 1
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("FIRESTORE_RETRIES_NUMBER is missing, was set to: %v", firestoreRetriesNumber), "")
	}

	if !firestoreCollectionExists(collectionName) {
		return nil, fmt.Errorf("Collection name '%v' does not exist", collectionName)
	}

	for i := 0; i < firestoreRetriesNumber; i++ {
		docRef, _, err = fireclient.Collection(collectionName).Add(ctx, entity)
		if err == nil {
			return docRef, nil
		}

		if err.Error() == ClosingTransportError {
			//recreate fireclient connection
			_, fireclient, err = getFirestoreAppAndClientWithContext(ctx)
			if err != nil {
				return nil, fmt.Errorf("Error updating fireclient (%d - retries left): %v", firestoreRetriesNumber-i, err.Error())
			}

			//do retry if ClosingTransportError
			continue
		}

		//return if another error
		return nil, fmt.Errorf("Unsuccessful adding data to the '%v' collection, Error: %v", collectionName, err.Error())
	}

	// we got here if we exceed retries number
	return nil, fmt.Errorf("Exceed retries number for adding data to the '%v' collection, Error: %v", collectionName, err.Error())
}

// getEntityFromFirestore - gets any entity from the firestore collection with retries
// getting only one by one
func getEntityFromFirestore(ctx context.Context, fireclient *firestore.Client, collectionName, entityID string) (*firestore.DocumentSnapshot, error) {
	var doc *firestore.DocumentSnapshot

	firestoreRetriesNumber, err := strconv.Atoi(os.Getenv("FIRESTORE_RETRIES_NUMBER"))
	if err != nil {
		firestoreRetriesNumber = 1
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("FIRESTORE_RETRIES_NUMBER is missing, was set to: %v", firestoreRetriesNumber), "")
	}

	if entityID == "" {
		return nil, errors.New("entity ID is required field for get")
	}
	if !firestoreCollectionExists(collectionName) {
		return nil, fmt.Errorf("document name '%v' does not exist", collectionName)
	}

	for i := 0; i < firestoreRetriesNumber; i++ {
		doc, err = fireclient.Collection(collectionName).Doc(entityID).Get(ctx)
		if err == nil {
			return doc, nil
		}

		if err.Error() == ClosingTransportError || strings.Contains(err.Error(), "The service is temporarily unavailable") {
			LogWrite(LogTypeInfo, 0, fmt.Sprintf("failed to get data from the '%v' collection, Error: %v. Will do retry!", collectionName, err.Error()), "")

			// recreate fireclient connection
			_, fireclient, err = getFirestoreAppAndClientWithContext(ctx)
			if err != nil {
				return nil, fmt.Errorf("error updating fireclient (%d - retries left): %v", firestoreRetriesNumber-i, err.Error())
			}

			// do retry if ClosingTransportError or service unavailable error
			continue
		}

		// return if another error
		return nil, fmt.Errorf("unsuccessful getting data from the '%v' collection, Error: %v", collectionName, err.Error())
	}

	// we got here if we exceed retries number
	return nil, fmt.Errorf("Exceed retries number for getting data from the '%v' collection, Error: %v", collectionName, err.Error())
}

// editEntityInFirestore - edits any entity in the firestore collection with retries
func editEntityInFirestore(ctx context.Context, fireclient *firestore.Client, collectionName, entityID string, entity interface{}) error {
	firestoreRetriesNumber, err := strconv.Atoi(os.Getenv("FIRESTORE_RETRIES_NUMBER"))
	if err != nil {
		firestoreRetriesNumber = 1
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("FIRESTORE_RETRIES_NUMBER is missing, was set to: %v", firestoreRetriesNumber), "")
	}

	if entityID == "" {
		return errors.New("Entity ID is required field for edit")
	}
	if !firestoreCollectionExists(collectionName) {
		return fmt.Errorf("Document name '%v' does not exist", collectionName)
	}

	for i := 0; i < firestoreRetriesNumber; i++ {
		//MergeAll expects to use only mapped data
		_, err = fireclient.Collection(collectionName).Doc(entityID).Set(ctx, entity, firestore.MergeAll)
		if err == nil {
			return nil
		}

		if err.Error() == ClosingTransportError || strings.Contains(err.Error(), UnavailableServiceError) {
			//recreate fireclient connection
			_, fireclient, err = getFirestoreAppAndClientWithContext(ctx)
			if err != nil {
				return fmt.Errorf("Error updating fireclient (%d - retries left): %v", firestoreRetriesNumber-i, err.Error())
			}
			//do retry if ClosingTransportError
			continue
		}

		//return if another error
		return fmt.Errorf("Unsuccessful updating '%v' in the '%v' collection, Error: %v", entityID, collectionName, err.Error())
	}

	// we got here if we exceed retries number
	return fmt.Errorf("Exceed retries number for updating '%v' in the '%v' collection, Error: %v", entityID, collectionName, err.Error())
}

// deleteEntityFromFirestore - delets any entity from the firestore collection with retries
func deleteEntityFromFirestore(ctx context.Context, fireclient *firestore.Client, collectionName, entityID string) (*firestore.WriteResult, error) {
	var result *firestore.WriteResult

	firestoreRetriesNumber, err := strconv.Atoi(os.Getenv("FIRESTORE_RETRIES_NUMBER"))
	if err != nil {
		firestoreRetriesNumber = 1
		LogWrite(LogTypeInfo, 0, fmt.Sprintf("FIRESTORE_RETRIES_NUMBER is missing, was set to: %v", firestoreRetriesNumber), "")
	}

	if entityID == "" {
		return nil, errors.New("Entity ID is required field for deletion")
	}
	if !firestoreCollectionExists(collectionName) {
		return nil, errors.New("Document name does not exist")
	}

	for i := 0; i < firestoreRetriesNumber; i++ {
		result, err = fireclient.Collection(collectionName).Doc(entityID).Delete(ctx)
		if err != nil {
			if err.Error() == ClosingTransportError {
				//recreate fireclient connection
				_, fireclient, err = getFirestoreAppAndClientWithContext(ctx)
				if err != nil {
					return result, fmt.Errorf("Error updating fireclient (%d - retries left): %v", firestoreRetriesNumber-i, err.Error())
				}

				//do retry if ClosingTransportError
				continue
			}

			//return if another error
			return nil, fmt.Errorf("Unsuccessful deletion %v from %v collection, Error: %v", entityID, collectionName, err.Error())
		}

		return result, nil
	}

	// we got here if we exceed retries number
	return nil, fmt.Errorf("Exceed retries number for deletion %v from %v collection, Error: %v", entityID, collectionName, err.Error())
}

func getFirestoreAppAndClientWithContext(ctx context.Context) (*firebase.App, *firestore.Client, error) {
	fireapp, err := firebase.NewApp(ctx, nil)
	if err != nil {
		return nil, nil, err
	}

	fireclient, err := fireapp.Firestore(ctx)
	if err != nil {
		return nil, nil, err
	}

	return fireapp, fireclient, nil
}

// firestoreCollectionExists checks if collection present in the FirestoreCollectionNames list
func firestoreCollectionExists(docName string) bool {
	for _, name := range FirestoreCollectionNames {
		if name == docName {
			return true
		}
	}
	return false
}
