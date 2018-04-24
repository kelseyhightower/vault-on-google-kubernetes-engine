// Copyright 2018 Google Inc. All Rights Reserved.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2/google"
	cloudkms "google.golang.org/api/cloudkms/v1"
)

var (
	bucketName    string
	httpClient    http.Client
	interval      int
	storageClient *storage.Client
)

type initRequest struct {
	SecretShares    int `json:"secret_shares"`
	SecretThreshold int `json:"secret_threshold"`
}

type initResponse struct {
	Keys       []string `json:"keys"`
	KeysBase64 []string `json:"keys_base64"`
	RootToken  string   `json:"root_token"`
}

type unsealRequest struct {
	Key   string `json:"key"`
	Reset bool   `json:"reset"`
}

type unsealResponse struct {
	Sealed   bool `json:"sealed"`
	T        int  `json:"t"`
	N        int  `json:"n"`
	Progress int  `json:"progress"`
}

func main() {
	bucketName = os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		log.Fatal("BUCKET_NAME must be set and non empty")
	}

	var err error

	ctx := context.Background()
	storageClient, err = storage.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}

	httpClient = http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	for {
		response, err := httpClient.Get("https://127.0.0.1:8200/v1/sys/health")
		if err != nil {
			log.Println(err)
			time.Sleep(10 * time.Second)
			continue
		}

		switch response.StatusCode {
		case 200:
			log.Println("Vault is initialized and unsealed.")
		case 429:
			log.Println("Vault is unsealed and in standby mode.")
		case 501:
			log.Println("Vault is not initialized. Initializing...")
			initialize()
		case 503:
			log.Println("Vault is sealed. Unsealing...")
			unseal()
		default:
			log.Println("Vault is in an unknown state")
		}

		log.Println("Next check in 10 seconds.")
		time.Sleep(10 * time.Second)
	}
}

func initialize() {
	ir := initRequest{
		SecretShares:    1,
		SecretThreshold: 1,
	}

	data, err := json.Marshal(&ir)
	if err != nil {
		log.Println(err)
		return
	}

	request, err := http.NewRequest("PUT", "https://127.0.0.1:8200/v1/sys/init", bytes.NewReader(data))
	if err != nil {
		log.Println(err)
		return
	}

	response, err := httpClient.Do(request)
	if err != nil {
		log.Println(err)
		return
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Println(err)
		return
	}

	if response.StatusCode != 200 {
		log.Printf("Non 200 status code: %s", response.StatusCode)
		return
	}

	ctx := context.Background()
	client, err := google.DefaultClient(ctx, cloudkms.CloudPlatformScope)
	if err != nil {
		log.Println(err)
		return
	}

	kmsService, err := cloudkms.New(client)
	if err != nil {
		log.Println(err)
		return
	}

	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s",
		"hightowerlabs", "global", "vault", "vault-init")

	er := &cloudkms.EncryptRequest{
		Plaintext: base64.StdEncoding.EncodeToString(body),
	}

	log.Println("Encrypting Vault init keys")
	encryptResponse, err := kmsService.Projects.Locations.KeyRings.CryptoKeys.Encrypt(keyName, er).Do()
	if err != nil {
		log.Println(err)
		return
	}

	bucket := storageClient.Bucket(bucketName)

	ctx = context.Background()
	w := bucket.Object("keys.json").NewWriter(ctx)
	defer w.Close()

	_, err = w.Write([]byte(encryptResponse.Ciphertext))
	if err != nil {
		log.Println(err)
	}

	log.Printf("Keys written to gs://%s/%s", bucketName, "keys.json")
	log.Println("Initialization complete.")
}

func unseal() {
	bucket := storageClient.Bucket(bucketName)

	ctx := context.Background()
	r, err := bucket.Object("keys.json").NewReader(ctx)
	if err != nil {
		log.Println(err)
		return
	}

	defer r.Close()

	data, err := ioutil.ReadAll(r)
	if err != nil {
		log.Println(err)
		return
	}

	ctx = context.Background()
	client, err := google.DefaultClient(ctx, cloudkms.CloudPlatformScope)
	if err != nil {
		log.Println(err)
		return
	}

	kmsService, err := cloudkms.New(client)
	if err != nil {
		log.Println(err)
		return
	}

	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s",
		"hightowerlabs", "global", "vault", "vault-init")

	dr := &cloudkms.DecryptRequest{
		Ciphertext: string(data),
	}

	decryptResponse, err := kmsService.Projects.Locations.KeyRings.CryptoKeys.Decrypt(keyName, dr).Do()
	if err != nil {
		log.Println(err)
		return
	}

	var ir initResponse

	px, err := base64.StdEncoding.DecodeString(decryptResponse.Plaintext)
	if err != nil {
        log.Println(err)
        return
    }

	if err := json.Unmarshal(px, &ir); err != nil {
		log.Println(err)
		return
	}

	for _, key := range ir.KeysBase64 {
		ur := unsealRequest{
			Key: key,
		}

		b, err := json.Marshal(&ur)
		if err != nil {
			log.Println(err)
			break
		}

		request, err := http.NewRequest("PUT", "https://127.0.0.1:8200/v1/sys/unseal", bytes.NewReader(b))
		if err != nil {
			log.Println(err)
			break
		}

		response, err := httpClient.Do(request)
		if err != nil {
			log.Println(err)
			break
		}

		if response.StatusCode != 200 {
			log.Printf("Non 200 status code: %s", response.StatusCode)
			break
		}

		b, err = ioutil.ReadAll(response.Body)
		if err != nil {
			log.Println(err)
			break
		}

		var unr unsealResponse
		if err := json.Unmarshal(b, &unr); err != nil {
			log.Println(err)
			break
		}

		if !unr.Sealed {
			log.Println("Unseal complete.")
			break
		}
	}
}
