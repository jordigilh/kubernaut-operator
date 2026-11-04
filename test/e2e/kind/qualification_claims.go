/*
Copyright 2026 Jordi Gil.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kind

import "fmt"

// ValidateQualificationAdditionalClaims fails closed when the deployed
// OAuth2 Proxy userinfo response omits, changes type, or changes the expected
// iss, sub, or sid claims.
func ValidateQualificationAdditionalClaims(userInfo map[string]any, expectedSID, expectedSub string) error {
	claims, ok := userInfo["additionalClaims"].(map[string]any)
	if !ok {
		return fmt.Errorf("qualification additionalClaims is not an object")
	}
	issuer, ok := claims["iss"].(string)
	if !ok || issuer == "" {
		return fmt.Errorf("qualification iss claim is missing or malformed")
	}
	if issuer != qualificationKeycloakIssuer {
		return fmt.Errorf("qualification iss claim is mismatched")
	}
	subject, ok := claims["sub"].(string)
	if !ok || subject == "" {
		return fmt.Errorf("qualification sub claim is missing or malformed")
	}
	if expectedSub != "" && subject != expectedSub {
		return fmt.Errorf("qualification sub claim is mismatched")
	}
	sid, ok := claims["sid"].(string)
	if !ok || sid == "" {
		return fmt.Errorf("qualification sid claim is missing or malformed")
	}
	if expectedSID != "" && sid != expectedSID {
		return fmt.Errorf("qualification sid claim is mismatched")
	}
	return nil
}
