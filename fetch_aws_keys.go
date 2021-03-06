// Licensed to the Apache Software Foundation (ASF) under one or more contributor license agreements;
// and to You under the Apache License, Version 2.0.  See LICENSE in project root for full license + copyright.

package keynuker

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/tleyden/keynuker/keynuker-go-common"
	"time"
)

// Look up all the AWS keys associated with the AWS account corresponding to AwsAccessKeyId
// and return a document suitable for sticking into a Cloudant database.
//
// This is meant to be run in the context of an OpenWhisk Action
// (see https://tleyden.github.io/blog/2017/07/02/openwhisk-action-sequences/)
// and so nothing else except the JSON content can be written to standard output.
func FetchAwsKeys(params ParamsFetchAwsKeys) (docWrapper DocumentWrapperFetchAwsKeys, err error) {

	docId := keynuker_go_common.GenerateDocId(
		keynuker_go_common.DocIdPrefixAwsKeys,
		params.KeyNukerOrg,
	)

	// Create output document + wrapepr
	doc := DocumentFetchAwsKeys{
		Id:                docId,
		AccessKeyMetadata: []FetchedAwsAccessKey{},
	}

	for _, targetAwsAccount := range params.TargetAwsAccounts {

		// Create AWS session
		sess, err := session.NewSession(&aws.Config{
			Credentials: credentials.NewCredentials(
				&credentials.StaticProvider{Value: credentials.Value{
					AccessKeyID:     targetAwsAccount.AwsAccessKeyId,
					SecretAccessKey: targetAwsAccount.AwsSecretAccessKey,
				}},
			),
		})
		if err != nil {
			return DocumentWrapperFetchAwsKeys{}, fmt.Errorf("Error creating aws session: %v", err)
		}

		// Create IAM client with the session.
		svc := iam.New(sess)

		// Fetch list of IAM users
		iamUsers, err := FetchIAMUsers(svc)
		if err != nil {
			return DocumentWrapperFetchAwsKeys{}, err
		}

		for _, user := range iamUsers {

			listAccessKeysInput := &iam.ListAccessKeysInput{
				UserName: user.UserName,
				MaxItems: aws.Int64(1000),
			}

			listAccessKeysOutput, err := svc.ListAccessKeys(listAccessKeysInput)
			if err != nil {
				return DocumentWrapperFetchAwsKeys{}, fmt.Errorf("Error listing access keys for user: %v.  Err: %v", user, err)
			}

			// Panic if more than 1K results, which is not handled
			if *listAccessKeysOutput.IsTruncated {
				// TODO: remove panic and put in a paginated loop.  Move to tleyden/awsutils + unit tests against mocks
				return DocumentWrapperFetchAwsKeys{}, fmt.Errorf("Output is truncated and this code does not handle it")
			}

			for _, accessKeyMetadata := range listAccessKeysOutput.AccessKeyMetadata {

				fetchedAwsAccessKey := NewFetchedAwsAccessKey(
					accessKeyMetadata,
					targetAwsAccount.AwsAccessKeyId,
				)

				doc.AccessKeyMetadata = append(doc.AccessKeyMetadata, *fetchedAwsAccessKey)
			}

		}

	}

	docWrapper = DocumentWrapperFetchAwsKeys{
		Doc:   doc,
		DocId: docId,
	}

	return docWrapper, nil

}

func FetchIAMUsers(svc *iam.IAM) (users []*iam.User, err error) {

	// Discover list of IAM users in account
	listUsersInput := &iam.ListUsersInput{
		MaxItems: aws.Int64(1000),
	}
	listUsersOutput, err := svc.ListUsers(listUsersInput)
	if err != nil {
		return nil, fmt.Errorf("Error listing users: %v", err)
	}
	// Panic if more than 1K results, which is not handled
	if *listUsersOutput.IsTruncated {
		// TODO: remove panic and put in a paginated loop.  Move to tleyden/awsutils + unit tests against mocks
		return nil, fmt.Errorf("Output is truncated and this code does not handle it")
	}

	return listUsersOutput.Users, nil

}

type TargetAwsAccount struct {

	// The aws access key to connect as.  This only needs permissions to list IAM users and access keys,
	// and delete access keys (in the case they are nuked)
	AwsAccessKeyId string

	// The secret access key corresponding to AwsAccessKeyId
	AwsSecretAccessKey string
}

type ParamsFetchAwsKeys struct {

	// The list of AWS accounts to fetch all the access keys for
	TargetAwsAccounts []TargetAwsAccount

	// This is the name of the KeyNuker "org/tenant".  Defaults to "default", but allows to be extended multi-tenant.
	KeyNukerOrg string
}

// This encapsulates all of the fields from iam.AccessKeyMetadata, as well as addding the FetcherAwsAccessKeyId
// that was used to fetch (and should be used to nuke key if needed)
type FetchedAwsAccessKey struct {

	// The ID for this access key.
	AccessKeyId *string `min:"16" type:"string"`

	// The date when the access key was created.
	CreateDate *time.Time `type:"timestamp" timestampFormat:"iso8601"`

	// The status of the access key. Active means the key is valid for API calls;
	// Inactive means it is not.
	Status *string `type:"string" enum:"statusType"`

	// The name of the IAM user that the key is associated with.
	UserName *string `min:"1" type:"string"`

	// The AWS access key used to monitor this AWS account's keys.  Need to track since this same key will need to be used to nuke as well
	MonitorAwsAccessKeyId string
}

// Create a new FetchedAwsAccessKey
func NewFetchedAwsAccessKey(accessKeyMetadata *iam.AccessKeyMetadata, monitorAwsAccessKeyId string) *FetchedAwsAccessKey {
	return &FetchedAwsAccessKey{
		AccessKeyId:           accessKeyMetadata.AccessKeyId,
		CreateDate:            accessKeyMetadata.CreateDate,
		Status:                accessKeyMetadata.Status,
		UserName:              accessKeyMetadata.UserName,
		MonitorAwsAccessKeyId: monitorAwsAccessKeyId,
	}
}

type DocumentFetchAwsKeys struct {
	Id string `json:"_id"`

	AccessKeyMetadata []FetchedAwsAccessKey
}

type DocumentWrapperFetchAwsKeys struct {
	// Serialize into a form that the cloudant db adapter expects
	Doc   DocumentFetchAwsKeys `json:"doc"`
	DocId string               `json:"docid"`
}
