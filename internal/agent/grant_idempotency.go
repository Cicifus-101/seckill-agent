package agent

import (
	"fmt"
)

// BuildReviewCouponGrantIdempotencyKey
func BuildReviewCouponGrantIdempotencyKey(reviewID, storeID int64) string {
	return fmt.Sprintf("review:store:%d:review:%d", storeID, reviewID)
}
