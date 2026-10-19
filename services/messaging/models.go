package messaging

import "time"

type Conversation struct {
	ID          string    `json:"id"`
	ListingID   string    `json:"listing_id"`
	BuyerID     string    `json:"buyer_id"`
	SellerID    string    `json:"seller_id"`
	Title       string    `json:"title"`
	PeerName    string    `json:"peer_name"`
	LastMessage string    `json:"last_message"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Message struct {
	ID             string    `json:"id"`
	Seq            int64     `json:"seq"`
	ConversationID string    `json:"conversation_id"`
	SenderID       string    `json:"sender_id"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}
