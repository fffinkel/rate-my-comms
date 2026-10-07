package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoStore keeps one item per vote. Partition key is the communication
// key, sort key is the vote id, so results for one key are a single Query.
// Listing every key is a Scan, which is fine at this volume.
type DynamoStore struct {
	db    *dynamodb.Client
	table string
}

type dynamoItem struct {
	Key     string `dynamodbav:"key"`
	ID      string `dynamodbav:"id"`
	Value   string `dynamodbav:"value"`
	Comment string `dynamodbav:"comment,omitempty"`
	At      string `dynamodbav:"at"`
}

// OpenDynamoStore uses the default AWS credential and region chain.
func OpenDynamoStore(ctx context.Context, table string) (*DynamoStore, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &DynamoStore{db: dynamodb.NewFromConfig(cfg), table: table}, nil
}

func (s *DynamoStore) Close() error { return nil }

func (s *DynamoStore) Add(ctx context.Context, key, value string) (string, error) {
	id := newID()
	item, err := attributevalue.MarshalMap(dynamoItem{
		Key: key, ID: id, Value: value, At: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return "", err
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{TableName: &s.table, Item: item})
	return id, err
}

func (s *DynamoStore) SetComment(ctx context.Context, id, key, comment string) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &s.table,
		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
			"id":  &types.AttributeValueMemberS{Value: id},
		},
		UpdateExpression:         aws.String("SET #c = :c"),
		ConditionExpression:      aws.String("attribute_exists(id)"),
		ExpressionAttributeNames: map[string]string{"#c": "comment"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":c": &types.AttributeValueMemberS{Value: comment},
		},
	})
	var cond *types.ConditionalCheckFailedException
	if errors.As(err, &cond) {
		return ErrNotFound
	}
	return err
}

func (s *DynamoStore) ByKey(ctx context.Context, key string) ([]Vote, error) {
	p := dynamodb.NewQueryPaginator(s.db, &dynamodb.QueryInput{
		TableName:                &s.table,
		KeyConditionExpression:   aws.String("#k = :k"),
		ExpressionAttributeNames: map[string]string{"#k": "key"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":k": &types.AttributeValueMemberS{Value: key},
		},
	})
	var out []Vote
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		votes, err := toVotes(page.Items)
		if err != nil {
			return nil, err
		}
		out = append(out, votes...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

func (s *DynamoStore) Keys(ctx context.Context) ([]KeyCount, error) {
	p := dynamodb.NewScanPaginator(s.db, &dynamodb.ScanInput{
		TableName:                &s.table,
		ProjectionExpression:     aws.String("#k, #a"),
		ExpressionAttributeNames: map[string]string{"#k": "key", "#a": "at"},
	})
	m := map[string]*KeyCount{}
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		votes, err := toVotes(page.Items)
		if err != nil {
			return nil, err
		}
		for _, v := range votes {
			kc, ok := m[v.Key]
			if !ok {
				kc = &KeyCount{Key: v.Key}
				m[v.Key] = kc
			}
			kc.Count++
			if v.At.After(kc.Last) {
				kc.Last = v.At
			}
		}
	}
	out := make([]KeyCount, 0, len(m))
	for _, kc := range m {
		out = append(out, *kc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out, nil
}

func toVotes(items []map[string]types.AttributeValue) ([]Vote, error) {
	var raw []dynamoItem
	if err := attributevalue.UnmarshalListOfMaps(items, &raw); err != nil {
		return nil, err
	}
	out := make([]Vote, 0, len(raw))
	for _, it := range raw {
		at, err := time.Parse(time.RFC3339Nano, it.At)
		if err != nil {
			return nil, fmt.Errorf("vote %s: bad timestamp %q", it.ID, it.At)
		}
		out = append(out, Vote{ID: it.ID, Key: it.Key, Value: it.Value, Comment: it.Comment, At: at})
	}
	return out, nil
}
