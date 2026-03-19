package otp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"ohmf/services/gateway/internal/config"
)

type Provider interface {
	SendCode(ctx context.Context, phoneE164, code string) error
	Name() string
}

type DevProvider struct{}

func (DevProvider) Name() string { return "dev" }

func (DevProvider) SendCode(_ context.Context, phoneE164, code string) error {
	log.Printf("otp.dev: %s -> %s", phoneE164, code)
	return nil
}

type TwilioSMSProvider struct {
	accountSID       string
	authToken        string
	from             string
	messagingService string
	httpClient       *http.Client
}

func (p *TwilioSMSProvider) Name() string { return "twilio_sms" }

func (p *TwilioSMSProvider) SendCode(ctx context.Context, phoneE164, code string) error {
	if strings.TrimSpace(phoneE164) == "" {
		return errors.New("phone_required")
	}
	form := url.Values{}
	form.Set("To", phoneE164)
	form.Set("Body", fmt.Sprintf("Your OHMF verification code is %s", code))
	if p.messagingService != "" {
		form.Set("MessagingServiceSid", p.messagingService)
	} else {
		form.Set("From", p.from)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", p.accountSID),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.accountSID, p.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("twilio_send_failed:%d", resp.StatusCode)
	}
	return nil
}

func NewProvider(cfg config.Config) (Provider, error) {
	if !cfg.UseRealOTPProvider {
		return DevProvider{}, nil
	}
	switch strings.TrimSpace(strings.ToLower(cfg.OTPProvider)) {
	case "", "dev":
		return DevProvider{}, nil
	case "twilio_sms":
		if cfg.TwilioAccountSID == "" || cfg.TwilioAuthToken == "" {
			return nil, errors.New("missing_twilio_credentials")
		}
		if cfg.TwilioMessagingService == "" && cfg.OTPFrom == "" {
			return nil, errors.New("missing_twilio_sender")
		}
		return &TwilioSMSProvider{
			accountSID:       cfg.TwilioAccountSID,
			authToken:        cfg.TwilioAuthToken,
			from:             cfg.OTPFrom,
			messagingService: cfg.TwilioMessagingService,
			httpClient:       &http.Client{},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported_otp_provider:%s", cfg.OTPProvider)
	}
}
