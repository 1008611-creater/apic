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

package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type RedeemSubscriptionCodeRequest struct {
	Code string `json:"code"`
}

type GenerateSubscriptionRedeemCodesRequest struct {
	PlanId    int   `json:"plan_id"`
	Count     int   `json:"count"`
	ExpiresAt int64 `json:"expires_at"`
}

func RedeemSubscriptionCode(c *gin.Context) {
	var req RedeemSubscriptionCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Code) == "" || len(req.Code) > 64 {
		common.ApiErrorMsg(c, "\u65e0\u6548\u7684\u5361\u5bc6\u7f16\u53f7")
		return
	}
	subscription, err := model.RedeemSubscriptionCode(req.Code, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"subscription": subscription})
}

func AdminGenerateSubscriptionRedeemCodes(c *gin.Context) {
	var req GenerateSubscriptionRedeemCodesRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 || req.Count < 1 || req.Count > model.MaxSubscriptionRedeemBatchSize {
		common.ApiErrorMsg(c, "\u53c2\u6570\u9519\u8bef\uff1a\u6bcf\u6279\u53ef\u751f\u6210 1 \u5230 1000 \u4e2a\u5361\u5bc6")
		return
	}
	batchId, codes, err := model.GenerateSubscriptionRedeemCodes(req.PlanId, req.Count, req.ExpiresAt, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// This is the only response that contains plaintext. It is never persisted.
	common.ApiSuccess(c, gin.H{"batch_id": batchId, "codes": codes})
}

func AdminListSubscriptionRedeemCodes(c *gin.Context) {
	start, _ := strconv.Atoi(c.DefaultQuery("start", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if start < 0 || limit < 1 || limit > 200 {
		common.ApiErrorMsg(c, "\u5206\u9875\u53c2\u6570\u9519\u8bef")
		return
	}
	codes, total, err := model.ListSubscriptionRedeemCodes(start, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": codes, "total": total})
}

func AdminRevokeSubscriptionRedeemCode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "Invalid code ID")
		return
	}
	if err := model.RevokeSubscriptionRedeemCode(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
