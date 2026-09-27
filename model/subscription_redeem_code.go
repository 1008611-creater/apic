/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	SubscriptionRedeemCodeIssued   = "issued"
	SubscriptionRedeemCodeRedeemed = "redeemed"
	SubscriptionRedeemCodeRevoked  = "revoked"
	MaxSubscriptionRedeemBatchSize = 1000
)

var (
	ErrSubscriptionRedeemCodeInvalid     = errors.New("\u5151\u6362\u7801\u65e0\u6548\u3001\u5df2\u8fc7\u671f\u6216\u5df2\u4f7f\u7528")
	ErrSubscriptionRedeemPlanUnavailable = errors.New("\u5151\u6362\u7801\u5bf9\u5e94\u7684\u5957\u9910\u4e0d\u5b58\u5728")
)

// SubscriptionRedeemCode stores only a digest of the one-time code. The plaintext
// is returned to the administrator only by the batch creation call.
type SubscriptionRedeemCode struct {
	Id             int    `json:"id"`
	CodeHash       string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	PlanId         int    `json:"plan_id" gorm:"index;not null"`
	BatchId        string `json:"batch_id" gorm:"type:char(32);index;not null"`
	CreatedBy      int    `json:"created_by" gorm:"index;not null"`
	Status         string `json:"status" gorm:"type:varchar(16);index;not null"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;not null"`
	ExpiresAt      int64  `json:"expires_at" gorm:"bigint;not null;default:0"`
	RedeemedAt     int64  `json:"redeemed_at" gorm:"bigint;not null;default:0"`
	RedeemedBy     int    `json:"redeemed_by" gorm:"index;not null;default:0"`
	SubscriptionId int    `json:"subscription_id" gorm:"index;not null;default:0"`
}

type SubscriptionRedeemCodeIssue struct {
	Code string `json:"code"`
}

func normalizeSubscriptionRedeemCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func hashSubscriptionRedeemCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeSubscriptionRedeemCode(code)))
	return hex.EncodeToString(sum[:])
}

func newSubscriptionRedeemCode() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)
	parts := make([]string, 0, 5)
	for len(encoded) > 0 {
		n := 8
		if len(encoded) < n {
			n = len(encoded)
		}
		parts = append(parts, encoded[:n])
		encoded = encoded[n:]
	}
	return "APIC-" + strings.Join(parts, "-"), nil
}

func newSubscriptionRedeemBatchId() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateSubscriptionRedeemCodes creates one-time codes for a fixed plan. The
// database receives only SHA-256 digests; plaintext codes are returned once.
func GenerateSubscriptionRedeemCodes(planId int, count int, expiresAt int64, createdBy int) (string, []SubscriptionRedeemCodeIssue, error) {
	if planId <= 0 || createdBy <= 0 || count < 1 || count > MaxSubscriptionRedeemBatchSize {
		return "", nil, errors.New("invalid subscription redemption batch")
	}
	now := common.GetTimestamp()
	if expiresAt != 0 && expiresAt <= now {
		return "", nil, errors.New("expiration must be in the future")
	}
	var plan SubscriptionPlan
	if err := DB.Where("id = ? AND enabled = ?", planId, true).First(&plan).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, errors.New("enabled subscription plan not found")
		}
		return "", nil, err
	}
	batchId, err := newSubscriptionRedeemBatchId()
	if err != nil {
		return "", nil, err
	}
	codes := make([]SubscriptionRedeemCodeIssue, 0, count)
	rows := make([]SubscriptionRedeemCode, 0, count)
	seen := make(map[string]struct{}, count)
	for i := 0; i < count; i++ {
		var code string
		for attempt := 0; attempt < 5; attempt++ {
			code, err = newSubscriptionRedeemCode()
			if err != nil {
				return "", nil, err
			}
			digest := hashSubscriptionRedeemCode(code)
			if _, ok := seen[digest]; !ok {
				seen[digest] = struct{}{}
				rows = append(rows, SubscriptionRedeemCode{
					CodeHash: digest, PlanId: planId, BatchId: batchId, CreatedBy: createdBy,
					Status: SubscriptionRedeemCodeIssued, CreatedAt: now, ExpiresAt: expiresAt,
				})
				codes = append(codes, SubscriptionRedeemCodeIssue{Code: code})
				break
			}
		}
		if len(codes) != i+1 {
			return "", nil, errors.New("could not generate unique redemption codes")
		}
	}
	if err := DB.Transaction(func(tx *gorm.DB) error { return tx.Create(&rows).Error }); err != nil {
		return "", nil, err
	}
	return batchId, codes, nil
}

// RedeemSubscriptionCode atomically consumes the code and creates the matching
// subscription. Any subscription creation failure rolls the code update back.
func RedeemSubscriptionCode(code string, userId int) (*UserSubscription, error) {
	normalized := normalizeSubscriptionRedeemCode(code)
	if normalized == "" || userId <= 0 {
		return nil, ErrSubscriptionRedeemCodeInvalid
	}
	digest := hashSubscriptionRedeemCode(normalized)
	now := common.GetTimestamp()
	subscriptionStartTime := GetDBTimestamp()
	var created *UserSubscription
	err := DB.Transaction(func(tx *gorm.DB) error {
		var redeemCode SubscriptionRedeemCode
		query := lockForUpdate(tx).Where("code_hash = ?", digest)
		if err := query.First(&redeemCode).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionRedeemCodeInvalid
			}
			return err
		}
		if redeemCode.Status != SubscriptionRedeemCodeIssued || (redeemCode.ExpiresAt != 0 && redeemCode.ExpiresAt <= now) {
			return ErrSubscriptionRedeemCodeInvalid
		}
		var plan SubscriptionPlan
		if err := tx.Where("id = ?", redeemCode.PlanId).First(&plan).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSubscriptionRedeemPlanUnavailable
			}
			return err
		}
		result := tx.Model(&SubscriptionRedeemCode{}).
			Where("id = ? AND status = ? AND (expires_at = 0 OR expires_at > ?)", redeemCode.Id, SubscriptionRedeemCodeIssued, now).
			Updates(map[string]interface{}{"status": SubscriptionRedeemCodeRedeemed, "redeemed_at": now, "redeemed_by": userId})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrSubscriptionRedeemCodeInvalid
		}
		createdSubscription, err := createUserSubscriptionFromPlanTxAt(tx, userId, &plan, "redeem_code", subscriptionStartTime)
		if err != nil {
			return err
		}
		if err := tx.Model(&SubscriptionRedeemCode{}).Where("id = ?", redeemCode.Id).
			Update("subscription_id", createdSubscription.Id).Error; err != nil {
			return err
		}
		created = createdSubscription
		return nil
	})
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, fmt.Errorf("subscription redemption completed without a subscription")
	}
	refreshSubscriptionUserGroupCache(userId, "subscription redemption")
	return created, nil
}

func ListSubscriptionRedeemCodes(startIdx int, num int) ([]SubscriptionRedeemCode, int64, error) {
	if startIdx < 0 || num < 1 || num > 200 {
		return nil, 0, errors.New("invalid pagination")
	}
	var total int64
	if err := DB.Model(&SubscriptionRedeemCode{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var codes []SubscriptionRedeemCode
	if err := DB.Order("id desc").Limit(num).Offset(startIdx).Find(&codes).Error; err != nil {
		return nil, 0, err
	}
	return codes, total, nil
}

func RevokeSubscriptionRedeemCode(id int) error {
	if id <= 0 {
		return errors.New("invalid redemption code id")
	}
	result := DB.Model(&SubscriptionRedeemCode{}).
		Where("id = ? AND status = ?", id, SubscriptionRedeemCodeIssued).
		Update("status", SubscriptionRedeemCodeRevoked)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("code not found or no longer redeemable")
	}
	return nil
}
