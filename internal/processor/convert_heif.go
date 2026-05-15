//go:build heif

// Package processor's libheif-backed decoders and AVIF encoder.
//
// Build with `-tags heif`. Requires libheif, libaom, and libde265 at
// build time; the Dockerfile's `prod` stage ships the runtime shared
// objects. When the tag is absent, convert_noheif.go provides stubs so
// the package still compiles (see that file for the degraded behaviour).
package processor

import (
	"image"
	"image/color"
	"io"

	libheif "github.com/strukturag/libheif-go"
)

func init() {
	// ISO BMFF containers: first 4 bytes are box size (variable), then
	// the literal "ftyp", then a 4-byte brand. '?' wildcards in the
	// magic string match any byte, so we anchor on "ftyp" + brand.
	for _, brand := range []string{"heic", "heix", "mif1", "msf1", "avif", "avis"} {
		image.RegisterFormat("heif-"+brand, "????ftyp"+brand, decodeHEIF, decodeConfigHEIF)
	}
}

func decodeHEIF(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	ctx, err := libheif.NewContext()
	if err != nil {
		return nil, err
	}
	if err := ctx.ReadFromMemory(data); err != nil {
		return nil, err
	}
	handle, err := ctx.GetPrimaryImageHandle()
	if err != nil {
		return nil, err
	}
	img, err := handle.DecodeImage(libheif.ColorspaceRGB, libheif.ChromaInterleavedRGBA, nil)
	if err != nil {
		return nil, err
	}
	return img.GetImage()
}

func decodeConfigHEIF(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, err
	}
	ctx, err := libheif.NewContext()
	if err != nil {
		return image.Config{}, err
	}
	if err := ctx.ReadFromMemory(data); err != nil {
		return image.Config{}, err
	}
	handle, err := ctx.GetPrimaryImageHandle()
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: color.RGBAModel,
		Width:      handle.GetWidth(),
		Height:     handle.GetHeight(),
	}, nil
}

func encodeAVIF(w io.Writer, img image.Image, quality int) error {
	ctx, _, err := libheif.EncodeFromImage(img, libheif.CompressionAV1, libheif.SetEncoderQuality(quality))
	if err != nil {
		return err
	}
	return ctx.Write(w)
}
