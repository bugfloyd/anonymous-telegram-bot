package invitations

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/uuid"
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

func (repo *Repository) createInviter(inviter string, count uint32) (*Inviter, error) {
	i := Inviter{
		ItemID:          "INVITER#" + uuid.New().String(),
		Inviter:         inviter,
		InvitationsLeft: count,
		InvitationsUsed: 0,
	}
	err := repo.table.Put(i).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create inviter: %w", err)
	}
	return &i, nil
}

func (repo *Repository) createInvitation(code string, inviter string, count uint32) (*Invitation, error) {
	i := Invitation{
		ItemID:          "INVITATION#" + code,
		Inviter:         inviter,
		InvitationsLeft: count,
		InvitationsUsed: 0,
	}
	err := repo.table.Put(i).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create invitation: %w", err)
	}
	return &i, nil
}

func (repo *Repository) readInviter(uuid string) (*Inviter, error) {
	var u Inviter
	err := repo.table.Get("ItemID", fmt.Sprintf("INVITER#%s", uuid)).One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get inviter: %w", err)
	}
	return &u, nil
}

func (repo *Repository) readInvitation(code string) (*Invitation, error) {
	var u Invitation
	err := repo.table.Get("ItemID", fmt.Sprintf("INVITATION#%s", code)).One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get invitation by id: %w", err)
	}
	return &u, nil
}

func (repo *Repository) readInvitationsByInviter(uuid string) (*[]Invitation, error) {
	var invitation []Invitation
	err := repo.table.Get("Inviter", uuid).Index("Inviter-GSI").Range("ItemID", dynamo.BeginsWith, "INVITATION").All(&invitation)
	if err != nil {
		return nil, fmt.Errorf("failed to get invitations by inviter: %w", err)
	}
	return &invitation, nil
}

func (repo *Repository) updateInviter(inviter *Inviter, updates map[string]interface{}) error {
	updateBuilder := repo.table.Update("ItemID", inviter.ItemID)
	for key, value := range updates {
		updateBuilder = updateBuilder.Set(key, value)
	}
	err := updateBuilder.Run()
	if err != nil {
		return fmt.Errorf("failed to update inviter: %w", err)
	}

	// Reflecting on user to update fields based on updates map
	val := reflect.ValueOf(inviter).Elem() // We use .Elem() to dereference the pointer to user
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
