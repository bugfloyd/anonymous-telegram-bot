#!/bin/bash

# Parameters
USER_UUID=$1
INVITATIONS_LEFT=$2

if [ -z "$USER_UUID" ] || [ -z "$INVITATIONS_LEFT" ]; then
    echo "Usage: $0 <SenderUUID> <InvitationsLeft>"
    exit 1
fi

aws dynamodb put-item --endpoint-url http://localhost:8000 --region eu-central-1 \
    --table-name AnonymousBot_Invitations \
    --item "{
        \"ItemID\": {\"S\": \"USER#$USER_UUID\"},
        \"UserUUID\": {\"S\": \"$USER_UUID\"},
        \"InvitationsLeft\": {\"N\": \"$INVITATIONS_LEFT\"},
        \"InvitationsUsed\": {\"N\": \"0\"},
        \"Type\": {\"S\": \"ZERO\"}
    }"

# Check if the command succeeded
if [ $? -eq 0 ]; then
    echo "Successfully added item with ID $USER_UUID to AnonymousBot_Invitations"
else
    echo "Error: Failed to add item to AnonymousBot_Invitations"
fi