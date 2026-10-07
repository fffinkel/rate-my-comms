package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// testStore runs the same behavior checks against any Store.
func testStore(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	id1, err := s.Add(ctx, "k1", "yes")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	id2, err := s.Add(ctx, "k1", "no")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, "k2", "3"); err != nil {
		t.Fatal(err)
	}

	if err := s.SetComment(ctx, id1, "k1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetComment(ctx, "missing", "k1", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetComment unknown id: err = %v, want ErrNotFound", err)
	}

	votes, err := s.ByKey(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if len(votes) != 2 || votes[0].ID != id1 || votes[1].ID != id2 {
		t.Fatalf("ByKey = %+v", votes)
	}
	if votes[0].Comment != "first" || votes[1].Comment != "" {
		t.Fatalf("comments = %q, %q", votes[0].Comment, votes[1].Comment)
	}
	if votes[0].Value != "yes" || votes[1].Value != "no" {
		t.Fatalf("values = %q, %q", votes[0].Value, votes[1].Value)
	}

	keys, err := s.Keys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].Key != "k2" || keys[0].Count != 1 || keys[1].Key != "k1" || keys[1].Count != 2 {
		t.Fatalf("Keys = %+v", keys)
	}
}

func TestFileStore(t *testing.T) {
	s, err := OpenFileStore(filepath.Join(t.TempDir(), "votes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	testStore(t, s)
}

// TestDynamoStore needs DynamoDB Local. Run it with:
//
//	docker run -d -p 8000:8000 amazon/dynamodb-local
//	AWS_ENDPOINT_URL_DYNAMODB=http://localhost:8000 AWS_REGION=us-east-1 \
//	  AWS_ACCESS_KEY_ID=x AWS_SECRET_ACCESS_KEY=x go test ./...
func TestDynamoStore(t *testing.T) {
	if os.Getenv("AWS_ENDPOINT_URL_DYNAMODB") == "" {
		t.Skip("AWS_ENDPOINT_URL_DYNAMODB not set")
	}
	ctx := context.Background()
	table := "rate-my-comms-test-" + newID()
	s, err := OpenDynamoStore(ctx, table)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   &table,
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("key"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("key"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("id"), KeyType: types.KeyTypeRange},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: &table}) })
	testStore(t, s)
}
