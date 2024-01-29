package cloudfunctions_go_utils

import (
	"cloud.google.com/go/firestore"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/diegosz/go-graphql-client"
	"github.com/fatih/structs"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	FCShippingSecretDataCollection string = "fc_shipping_secret_data"
)

// FCShippingSecretData - database model for saving shipping secret and key for each fulfilment center
type FCShippingSecretData struct {
	ID                  string    `json:"id" firestore:"id,omitempty" structs:"id,omitempty"`
	KeyName             string    `json:"key_name" firestore:"key_name,omitempty" structs:"key_name,omitempty"`
	SecretName          string    `json:"secret_name" firestore:"secret_name,omitempty" structs:"secret_name,omitempty"`
	AccessToken         string    `json:"access_token" firestore:"access_token,omitempty" structs:"access_token,omitempty"`
	TokenExpirationDate time.Time `json:"token_expiration_date" firestore:"token_expiration_date,omitempty" structs:"token_expiration_date,omitempty"`
}

// getShippingSecretDataModel - returns Shipping secret data model by ID from DB
func getShippingSecretDataModel(ctx context.Context, fireclient *firestore.Client, ID string) (FCShippingSecretData, error) {
	dsnap, err := GetEntityFromFirestore(ctx, fireclient, FCShippingSecretDataCollection, ID)
	if err != nil {
		return FCShippingSecretData{}, fmt.Errorf("failed to get shipping secret data by ID %v. Error: %v", ID, err.Error())
	}

	secretDataBytes, err := json.Marshal(dsnap.Data())
	if err != nil {
		return FCShippingSecretData{}, fmt.Errorf("failed to marshal data. Error: %v", err.Error())
	}

	var secretData FCShippingSecretData
	err = json.Unmarshal(secretDataBytes, &secretData)
	if err != nil {
		return FCShippingSecretData{}, fmt.Errorf("failed to unmarshal data. Error: %v", err.Error())
	}

	return secretData, nil
}

// getIEAccessToken - return Imprint Engine access token from DB or regenerates it by API
func getIEAccessToken(ctx context.Context, fireclient *firestore.Client, apiCredentialsID string) (string, error) {
	secretDataModel, err := getShippingSecretDataModel(ctx, fireclient, apiCredentialsID)
	if err != nil {
		return "", fmt.Errorf("failed to get secretDataModel. Error: %v", err.Error())
	}

	if secretDataModel.TokenExpirationDate.Before(time.Now()) {
		newToken, err := renewImprintEngineAccessToken(ctx, secretDataModel.SecretName)
		if err != nil {
			return "", fmt.Errorf("failed to get Imprint Engine access token. Error: %v", err.Error())
		}

		secretDataModel.AccessToken = newToken
		// we add 20 hours instead of 24 to be sure that token wil not be expired earlier
		secretDataModel.TokenExpirationDate = time.Now().Add(time.Hour * time.Duration(20))

		err = EditEntityInFirestore(ctx, fireclient, FCShippingSecretDataCollection, secretDataModel.ID, structs.Map(secretDataModel))
		if err != nil {
			return "", fmt.Errorf("failed to update FCShippingSecretData. Error: %v", err.Error())
		}

		return newToken, nil
	}

	return secretDataModel.AccessToken, nil
}

func renewImprintEngineAccessToken(ctx context.Context, tokenSecretName string) (string, error) {
	refreshToken, err := GetSecret(ctx, tokenSecretName)
	if err != nil {
		return "", fmt.Errorf("failed to get refresh token from sercet manager. Error: %v", err.Error())
	}

	client := http.Client{}
	requestURL := os.Getenv("IMPRINT_ENGINE_AUTH_URL")

	req, err := http.NewRequest("POST", requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request. Error: %v", err.Error())
	}

	token := "Bearer " + refreshToken
	req.Header.Add("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request. Error: %v", err.Error())
	}

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("failed to get IE access token")
	}

	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body. Error: %v", err.Error())
	}

	ieAuthResponse := struct {
		AccessToken string `json:"accessToken"`
	}{}

	err = json.Unmarshal(respBody, &ieAuthResponse)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response body. Error: %v", err.Error())
	}

	return ieAuthResponse.AccessToken, nil
}

// getImprintEngineMNGraphQLClient - returns GraphQL client
func getImprintEngineMNGraphQLClient(ctx context.Context, fireclient *firestore.Client, apiCredentialsID string) (*graphql.Client, error) {
	token, err := getIEAccessToken(ctx, fireclient, apiCredentialsID)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token. Error: %v", err.Error())
	}

	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)

	postURL := os.Getenv("IMPRINT_ENGINE_GRAPHQL_URL")
	if postURL == "" {
		return nil, errors.New("IMPRINT_ENGINE_GRAPHQL_URL is empty")
	}

	httpClient := oauth2.NewClient(ctx, src)
	client := graphql.NewClient(postURL, httpClient)

	return client, nil
}

func getImprintEngineMNRequestConfig(ctx context.Context, fireclient *firestore.Client, orgID, apiCredentialsID string) (PreparedIEOrderData, error) {
	graphqlClient, err := getImprintEngineMNGraphQLClient(ctx, fireclient, apiCredentialsID)
	if err != nil {
		return PreparedIEOrderData{}, fmt.Errorf("failed to get ImprintEngine Client HTTP. Error: %v", err.Error())
	}

	appID, err := strconv.ParseInt(os.Getenv("IE_PLATFORM_APP_ID"), 10, 64)
	if err != nil {
		return PreparedIEOrderData{}, fmt.Errorf("failed parce IE_PLATFORM_APP_ID. Error: %v", err.Error())
	}

	if err != nil {
		return PreparedIEOrderData{}, fmt.Errorf("failed to get Organization fro DB. Error: %v", err.Error())
	}

	config := PreparedIEOrderData{
		Client: graphqlClient,
		AppID:  appID,
	}

	config.ExternalID, err = strconv.ParseInt(orgID, 10, 64) // parse the value into IE request format
	if err != nil {
		return PreparedIEOrderData{}, fmt.Errorf("invalid format of the external ID. Error: %v", err.Error())
	}

	return config, nil
}

// IEWarehouseMN - response model for getBluebirdAppID function
type IEWarehouseMN struct {
	AppID int64 `json:"app_id"`
}

type PreparedIEOrderData struct {
	Client     *graphql.Client
	AppID      int64
	ExternalID int64
}
