package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gateway-service/internal/domain"
)

type YandexIDClient interface {
	GetUser(ctx context.Context, oauthToken string) (domain.YandexUser, error)
}

// DevBypassClient accepts any non-empty token as one fixed Yandex ID without calling Yandex.
// It exists for testing stands only (DEV_AUTH_BYPASS_USER_ID).
type DevBypassClient struct {
	userID string
}

func NewDevBypassClient(userID string) *DevBypassClient {
	return &DevBypassClient{userID: userID}
}

func (c *DevBypassClient) GetUser(
	_ context.Context,
	oauthToken string,
) (domain.YandexUser, error) {
	if strings.TrimSpace(oauthToken) == "" {
		return domain.YandexUser{}, domain.ErrIdentityInvalid
	}

	return domain.YandexUser{ID: c.userID}, nil
}

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

type userInfoResponse struct {
	ID string `json:"id"`
}

func (c *Client) GetUser(
	ctx context.Context,
	oauthToken string,
) (domain.YandexUser, error) {
	oauthToken = strings.TrimSpace(oauthToken)
	if oauthToken == "" {
		return domain.YandexUser{}, domain.ErrIdentityInvalid
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL,
		nil,
	)
	if err != nil {
		return domain.YandexUser{}, fmt.Errorf(
			"create yandex user info request: %w",
			err,
		)
	}

	req.Header.Set(
		"Authorization",
		"OAuth "+oauthToken,
	)
	req.Header.Set(
		"Accept",
		"application/json",
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return domain.YandexUser{}, domain.ErrTimeout
		}

		return domain.YandexUser{}, fmt.Errorf(
			"request yandex user info: %w",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return domain.YandexUser{}, domain.ErrIdentityInvalid
		default:
			return domain.YandexUser{}, fmt.Errorf(
				"yandex user info: unexpected status %d",
				resp.StatusCode,
			)
		}
	}

	var result userInfoResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return domain.YandexUser{}, fmt.Errorf(
			"decode yandex user info: %w",
			err,
		)
	}

	if result.ID == "" {
		return domain.YandexUser{}, domain.ErrIdentityInvalid
	}

	return domain.YandexUser{
		ID: result.ID,
	}, nil
}
