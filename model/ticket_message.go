package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type TicketMessage struct {
	Id          int    `json:"id"`
	TicketId    int    `json:"ticket_id" gorm:"index;not null"`
	UserId      int    `json:"user_id" gorm:"index;not null"`
	Username    string `json:"username" gorm:"type:varchar(64)"`
	Role        int    `json:"role" gorm:"type:int"`
	Content     string `json:"content" gorm:"type:text;not null"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
}

func (message *TicketMessage) BeforeCreate(tx *gorm.DB) error {
	if message.CreatedTime == 0 {
		message.CreatedTime = common.GetTimestamp()
	}
	return nil
}
