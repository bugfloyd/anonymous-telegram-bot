#!/bin/bash

# Parameters
SENDER_UUID=$1
INVITATIONS_LEFT=$2

if [ -z "$SENDER_UUID" ] || [ -z "$INVITATIONS_LEFT" ]; then
    echo "Usage: $0 <SenderUUID> <InvitationsLeft>"
    exit 1
fi

# Generate a random ID
#ID=$(uuidgen)

aws dynamodb put-item --endpoint-url http://localhost:8000 --region eu-central-1 \
    --table-name AnonymousBot_Invitations \
    --item "{
        \"ItemID\": {\"S\": \"INVITER#$SENDER_UUID\"},
        \"Inviter\": {\"S\": \"$SENDER_UUID\"},
        \"InvitationsLeft\": {\"N\": \"$INVITATIONS_LEFT\"},
        \"InvitationsUsed\": {\"N\": \"0\"},
        \"Level\": {\"N\": \"1\"}
    }"

# Create the new item in the DynamoDB table without empty sets
#aws dynamodb put-item --endpoint-url http://localhost:8000 --region eu-central-1 \
#    --table-name AnonymousBot_Invitations \
#    --item "{
#        \"ItemID\": {\"S\": \"$ID\"},
#        \"Inviter\": {\"S\": \"$SENDER_UUID\"},
#        \"InvitationsLeft\": {\"N\": \"$INVITATIONS_LEFT\"}
#    }"

# Check if the command succeeded
if [ $? -eq 0 ]; then
    echo "Successfully added item with ID $SENDER_UUID to AnonymousBot_Invitations"
else
    echo "Error: Failed to add item to AnonymousBot_Invitations"
fi