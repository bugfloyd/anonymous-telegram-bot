package invitations

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/guregu/dynamo"
	"os"
	"reflect"
)

type Repository struct {
	table dynamo.Table
}

func NewRepository() (*Repository, error) {
	var sess *session.Session
	customDynamoDbEndpoint := os.Getenv("DYNAMODB_ENDPOINT")
	awsRegion := os.Getenv("AWS_REGION")

	if customDynamoDbEndpoint != "" {
		sess = session.Must(session.NewSession(&aws.Config{
			Region:   aws.String(awsRegion),
			Endpoint: aws.String(customDynamoDbEndpoint),
		}))
	} else {
		sess = session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)}))
	}

	db := dynamo.New(sess)

	return &Repository{
		table: db.Table("AnonymousBot_Invitations"),
	}, nil
}

func (repo *Repository) createUser(userID string) (*User, error) {
	i := User{
		ItemID:          "USER#" + userID,
		UserID:          userID,
		InvitationsLeft: 0,
		InvitationsUsed: 0,
		Type:            "NORMAL",
	}
	err := repo.table.Put(i).Run()
	if err != nil {
		return nil, fmt.Errorf("invitations: failed to create user: %w", err)
	}
	return &i, nil
}

func (repo *Repository) createInvitation(code string, userID string, count uint32) (*Invitation, error) {
	i := Invitation{
		ItemID:          "INVITATION#" + code,
		UserID:          userID,
		InvitationsLeft: count,
		InvitationsUsed: 0,
	}
	err := repo.table.Put(i).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create invitation: %w", err)
	}
	return &i, nil
}

func (repo *Repository) readUser(userId string) (*User, error) {
	var u User
	err := repo.table.Get("ItemID", "USER#"+userId).One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *Repository) readInvitation(code string) (*Invitation, error) {
	var u Invitation
	err := repo.table.Get("ItemID", "INVITATION#"+code).One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get invitation by id: %w", err)
	}
	return &u, nil
}

func (repo *Repository) readInvitationsByUser(userID string) (*[]Invitation, error) {
	var invitation []Invitation
	err := repo.table.Get("UserID", userID).Index("UserID-GSI").Range("ItemID", dynamo.BeginsWith, "INVITATION").All(&invitation)
	if err != nil {
		return nil, fmt.Errorf("failed to get invitations by user: %w", err)
	}
	return &invitation, nil
}

func (repo *Repository) updateInviter(user *User, updates map[string]interface{}) error {
	updateBuilder := repo.table.Update("ItemID", user.ItemID)
	for key, value := range updates {
		updateBuilder = updateBuilder.Set(key, value)
	}
	err := updateBuilder.Run()
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Reflecting on user to update fields based on updates map
	val := reflect.ValueOf(user).Elem() // We use .Elem() to dereference the pointer to user
	for key, value := range updates {
		fieldVal := val.FieldByName(key)
		if fieldVal.IsValid() && fieldVal.CanSet() {
			// Ensure the value is of the correct type
			correctTypeValue := reflect.ValueOf(value)
			if correctTypeValue.Type().ConvertibleTo(fieldVal.Type()) {
				correctTypeValue = correctTypeValue.Convert(fieldVal.Type())
			}
			fieldVal.Set(correctTypeValue)
		}
	}

	return nil
}
