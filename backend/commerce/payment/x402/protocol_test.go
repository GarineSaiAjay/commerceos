package x402

import "testing"

func TestEncodeDecodeHeaderRoundTrips(t *testing.T) {
	original := Challenge{
		X402Version: ProtocolVersion,
		Accepts: []Requirements{
			{
				Scheme:            "exact",
				Network:           "base-sepolia",
				Asset:             "USDC",
				Amount:            "10000",
				PayTo:             "0xabc",
				Resource:          "/x402/priority-support",
				MaxTimeoutSeconds: 60,
			},
		},
	}

	header, err := EncodeHeader(original)
	if err != nil {
		t.Fatalf("EncodeHeader: %v", err)
	}
	if header == "" {
		t.Fatal("expected a non-empty encoded header")
	}

	decoded, err := DecodeHeader[Challenge](header)
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}

	if decoded.X402Version != original.X402Version {
		t.Fatalf("expected X402Version %d, got %d", original.X402Version, decoded.X402Version)
	}
	if len(decoded.Accepts) != 1 || decoded.Accepts[0] != original.Accepts[0] {
		t.Fatalf("expected Accepts to round-trip unchanged, got %+v", decoded.Accepts)
	}
}

func TestDecodeHeaderRejectsInvalidBase64(t *testing.T) {
	_, err := DecodeHeader[Challenge]("not valid base64!!!")
	if err == nil {
		t.Fatal("expected an error decoding invalid base64")
	}
}

func TestDecodeHeaderRejectsInvalidJSON(t *testing.T) {
	// Valid base64, but the decoded bytes aren't JSON.
	_, err := DecodeHeader[Challenge]("bm90IGpzb24=") // base64("not json")
	if err == nil {
		t.Fatal("expected an error decoding non-JSON payload")
	}
}
