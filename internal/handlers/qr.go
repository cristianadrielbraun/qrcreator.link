package handlers

import (
	"crypto/rand"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
	"github.com/yeqown/go-qrcode/writer/standard/shapes"
)

// normalizeHTTPURL validates and normalizes a URL string for QR generation.
// It ensures an http/https scheme, a non-empty hostname, and returns a cleaned absolute URL.
func normalizeHTTPURL(s string) (string, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return "", fmt.Errorf("URL parameter is required")
	}
	// If missing scheme, default to https
	if !strings.Contains(v, "://") {
		v = "https://" + v
	}
	u, err := url.ParseRequestURI(v)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are supported")
	}
	if u.Host == "" {
		return "", fmt.Errorf("URL must include a valid host")
	}
	// Optional: cap length to avoid abuse
	if len(v) > 4096 {
		return "", fmt.Errorf("URL is too long")
	}
	return u.String(), nil
}

// min4 returns the minimum of four integers.
func min4(a, b, c, d int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	if d < m {
		m = d
	}
	return m
}

// QRCodeHandler generates QR codes for URLs with advanced customization options
func (h *Handler) QRCodeHandler(c *gin.Context) {
	rawURL := strings.TrimSpace(c.Query("url"))
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL parameter is required"})
		return
	}

	normalizedURL, err := normalizeHTTPURL(rawURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse format parameter (default to PNG)
	format := strings.ToLower(c.DefaultQuery("format", "png"))
	if format == "jpeg" {
		format = "jpg"
	}
	if format != "png" && format != "svg" && format != "jpg" {
		format = "png"
	}

	// Parse customization parameters
	colorMode := c.DefaultQuery("colorMode", "flat")
	bgColor := parseColorParam(c.Query("bg"), color.RGBA{255, 255, 255, 255}) // Default white
	cornerStyle := c.DefaultQuery("cornerStyle", "none")
	borderPattern := c.DefaultQuery("borderPattern", "simple")
	borderColorParam := c.Query("borderColor")
	// Combine corner style and border pattern
	var frame string
	switch cornerStyle {
	case "none":
		frame = "none"
	case "rounded":
		frame = "rounded-" + borderPattern
	default:
		frame = borderPattern
	}

	// Fixed 7% padding
	border := 7

	// Base frame width percent
	frameWidthPercent := 4
	// Make rounded frames start thicker before carving so final result
	// remains visually strong after the rounded inner cut.
	if strings.HasPrefix(frame, "rounded-") {
		frameWidthPercent = 6 // effective ~4% after inner carve
	}

	// Parse size parameter for different resolutions
	size := c.DefaultQuery("size", "preview") // "preview" or "download"

	// Basic request debug info
	fmt.Printf("[QR] request start: url=%q format=%s size=%s colorMode=%s qrShape=%s branding=%s\n",
		normalizedURL, format, size, c.DefaultQuery("colorMode", "flat"), c.DefaultQuery("qrShape", "rectangle"), c.DefaultQuery("branding", "default"))

	// Handle color mode
	var useGradient bool
	var gradient *standard.LinearGradient
	var fgColor color.RGBA
	var gradientStartColor color.RGBA

	if colorMode == "gradient" {
		// Parse gradient colors
		startColor := parseColorParam(c.Query("gradientStart"), color.RGBA{0, 0, 0, 255})
		middleColor := parseColorParam(c.Query("gradientMiddle"), color.RGBA{128, 128, 128, 255})
		endColor := parseColorParam(c.Query("gradientEnd"), color.RGBA{255, 0, 0, 255})

		// Store start color for logo branding
		gradientStartColor = startColor

		// Create gradient with 45-degree angle
		gradient = standard.NewGradient(45, []standard.ColorStop{
			{T: 0, Color: startColor},
			{T: 0.5, Color: middleColor},
			{T: 1, Color: endColor},
		}...)
		useGradient = true
	} else {
		// Flat color mode
		fgColor = parseColorParam(c.Query("fg"), color.RGBA{0, 0, 0, 255})
		useGradient = false
	}

	// Parse border color - use foreground/gradient start as default
	var borderColor color.RGBA
	if borderColorParam != "" {
		borderColor = parseColorParam(borderColorParam, color.RGBA{0, 0, 0, 255})
	} else {
		// Default to foreground color or gradient start color
		if useGradient {
			borderColor = gradientStartColor
		} else {
			borderColor = fgColor
		}
	}

	// Parse QR shape parameter
	qrShape := c.DefaultQuery("qrShape", "rectangle")

	// Parse branding parameters
	branding := c.DefaultQuery("branding", "default")
	customDomain := c.Query("customDomain")
	centerLogo := c.DefaultQuery("centerLogo", "false")
	logoFile := c.Query("logoFile")

	// Create QR code instance with Q error correction level
	qrc, err := qrcode.NewWith(normalizedURL, qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionQuart))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create QR code"})
		return
	}

	if format == "svg" {
		// Generate SVG format
		// Pass gradient colors if available
		var startColor, middleColor, endColor color.RGBA
		if useGradient {
			startColor = parseColorParam(c.Query("gradientStart"), color.RGBA{0, 0, 0, 255})
			middleColor = parseColorParam(c.Query("gradientMiddle"), color.RGBA{128, 128, 128, 255})
			endColor = parseColorParam(c.Query("gradientEnd"), color.RGBA{255, 0, 0, 255})
		}
		var _ string = branding
		var _ string = customDomain
		h.generateSVGQR(c, qrc, useGradient, fgColor, bgColor, startColor, middleColor, endColor, borderColor, border, frame, frameWidthPercent, size, qrShape, centerLogo)
	} else {
		// Generate PNG format (default)
		// Pass gradient colors if available
		var startColor, middleColor, endColor color.RGBA
		if useGradient {
			startColor = parseColorParam(c.Query("gradientStart"), color.RGBA{0, 0, 0, 255})
			middleColor = parseColorParam(c.Query("gradientMiddle"), color.RGBA{128, 128, 128, 255})
			endColor = parseColorParam(c.Query("gradientEnd"), color.RGBA{255, 0, 0, 255})
		}
		// Add debug header for quick inspection from devtools
		outFmt := format // png or jpg
		c.Header("X-QR-Debug", fmt.Sprintf("format=%s;size=%s;shape=%s;colorMode=%s", outFmt, size, qrShape, colorMode))
		h.generatePNGQR(c, qrc, useGradient, gradient, fgColor, bgColor, startColor, middleColor, endColor, borderColor, border, frame, frameWidthPercent, size, qrShape, centerLogo, logoFile, outFmt)
	}
}

// generatePNGQR generates a PNG QR code
func (h *Handler) generatePNGQR(c *gin.Context, qrc *qrcode.QRCode, useGradient bool, gradient *standard.LinearGradient, fgColor, bgColor, gradientStart, gradientMiddle, gradientEnd, borderColor color.RGBA, border int, frame string, frameWidthPercent int, size, qrShape, centerLogo, logoFile, outputFormat string) {
	// Create unique temporary file for PNG output
	tmpFile := filepath.Join(os.TempDir(), generateUniqueFilename("qr", ".png"))

	// Set module size based on requested size
	var moduleSize uint8
	if size == "download" {
		// For 2000x2000 target: 2000 / 21 modules ≈ 95 pixels per module
		// Let's use a large but safe value
		moduleSize = 120 // Should give us ~2520x2520 for typical QR
	} else {
		moduleSize = 16 // Slightly larger preview (336px target)
	}

	var writerOptions []standard.ImageOption
	baseOptions := []standard.ImageOption{
		standard.WithQRWidth(moduleSize),
		standard.WithBorderWidth(0), // Generate clean QR without borders
	}

	// Add center logo if requested
	if centerLogo == "true" {
		var logoPath string
		if logoFile != "" {
			// Use uploaded logo file
			logoPath = filepath.Join("uploads", logoFile)
		} else {
			// Use default uploaded logo
			logoPath = "uploads/temp_logo.png"
		}

		if _, err := os.Stat(logoPath); err == nil {
			baseOptions = append(baseOptions, standard.WithLogoImageFilePNG(logoPath))
		}
	}

	// Handle background color - transparent or solid
	if bgColor.A == 0 {
		// Transparent background
		fmt.Printf("DEBUG: Using transparent background for PNG\n")
		baseOptions = append(baseOptions, standard.WithBgTransparent())
		baseOptions = append(baseOptions, standard.WithBuiltinImageEncoder(standard.PNG_FORMAT))
	} else {
		// Solid background color
		fmt.Printf("DEBUG: Using solid background color: %+v\n", bgColor)
		baseOptions = append(baseOptions, standard.WithBgColor(bgColor))
	}

	// Add shape option based on qrShape parameter
	switch qrShape {
	case "circle":
		baseOptions = append(baseOptions, standard.WithCircleShape())
	case "liquid":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.LiquidBlock()}))
	case "chain":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.ChainBlock()}))
	case "hstripe":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.HStripeBlock(0.85)}))
	case "vstripe":
		baseOptions = append(baseOptions, standard.WithCustomShape(&customShape{drawFunc: shapes.VStripeBlock(0.85)}))
	default:
		// rectangle - default shape, no additional options needed
	}

	if useGradient {
		writerOptions = append(baseOptions, standard.WithFgGradient(gradient))
	} else {
		writerOptions = append(baseOptions, standard.WithFgColor(fgColor))
	}

	writer, err := standard.New(tmpFile, writerOptions...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create QR writer"})
		return
	}

	// Write QR code to file
	if err := qrc.Save(writer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate QR code image: %v", err)})
		return
	}

	// Clean up anti-aliasing artifacts (white border pixels) for transparent background
	if bgColor.A == 0 {
		if err := h.cleanupAntiAliasing(tmpFile, fgColor); err != nil {
			fmt.Printf("Warning: Failed to cleanup anti-aliasing: %v\n", err)
		}
	}

	// Ensure the writer is properly closed and synced
	writer.Close()

	// Debug: Check actual generated size
	if file, err := os.Open(tmpFile); err == nil {
		if img, _, err := image.DecodeConfig(file); err == nil {
			fmt.Printf("Generated QR size: %dx%d (requested moduleSize: %d, size: %s)\n",
				img.Width, img.Height, moduleSize, size)
		}
		file.Close()
	}

	// Force file system sync
	if file, err := os.OpenFile(tmpFile, os.O_RDWR, 0); err == nil {
		file.Sync()
		file.Close()
	}

	// For download size, ensure we reach target dimensions
	if size == "download" {
		if err := h.ensureMinimumQRSize(tmpFile, 2000); err != nil {
			fmt.Printf("Warning: Could not scale QR to target size: %v\n", err)
		}
	}

	// Store original QR size before any modifications
	originalSize := 0
	if file, err := os.Open(tmpFile); err == nil {
		if img, _, err := image.DecodeConfig(file); err == nil {
			originalSize = img.Width
		}
		file.Close()
	}

	// For preview: scale the base QR to a size that will produce the exact
	// requested previewSize AFTER padding and frame, so we don't scale the
	// decorative frame later (which causes aliasing/dotting artifacts).
	didPreviewPreScale := false
	if size == "preview" {
		if ps := c.Query("previewSize"); ps != "" {
			if target, err := strconv.Atoi(ps); err == nil && target > 0 && originalSize > 0 {
				// final = base + 2*(padding + frame) where padding = originalSize*border/100
				// and frame = originalSize*frameWidthPercent/100
				multiplier := 1.0 + 2.0*((float64(border)+float64(frameWidthPercent))/100.0)
				desiredBase := int(math.Round(float64(target) / multiplier))
				if desiredBase > 0 && desiredBase != originalSize {
					if err := h.ensureExactQRSize(tmpFile, desiredBase); err == nil {
						// update originalSize to the new base size
						if file, err := os.Open(tmpFile); err == nil {
							if img, _, err := image.DecodeConfig(file); err == nil {
								originalSize = img.Width
							}
							file.Close()
						}
						didPreviewPreScale = true
					}
				}
			}
		}
	}

	// Step 2: Add padding around QR+logo - use transparent padding for transparent QRs
	if border > 0 {
		paddingBgColor := bgColor
		if bgColor.A == 0 {
			paddingBgColor = color.RGBA{0, 0, 0, 0} // Ensure truly transparent
		}
		if err := h.addAbsolutePaddingToQRFile(tmpFile, border, originalSize, paddingBgColor); err != nil {
			fmt.Printf("Warning: Could not add padding to QR: %v\n", err)
		}
	}

	// Step 3: Add decorative frame around everything - with appropriate background
	if frame != "none" {
		framePixels := (originalSize * frameWidthPercent) / 100
		// Use the actual QR background color for the frame background so
		// any carved inner gap (rounded frames) visually matches the QR padding.
		frameBgColor := bgColor
		if bgColor.A == 0 {
			frameBgColor = color.RGBA{0, 0, 0, 0} // Ensure fully transparent
		}
		if err := h.addFrameToQRFile(tmpFile, frame, framePixels, frameBgColor, borderColor, useGradient, gradientStart, gradientMiddle, gradientEnd); err != nil {
			fmt.Printf("Warning: Could not add frame to QR: %v\n", err)
		}
	}

	// If preview and we did not pre-scale, fall back to final scaling as before
	if size == "preview" && !didPreviewPreScale {
		if ps := c.Query("previewSize"); ps != "" {
			if target, err := strconv.Atoi(ps); err == nil && target > 0 {
				if err := h.ensureExactQRSize(tmpFile, target); err != nil {
					fmt.Printf("Warning: Could not scale QR to preview size: %v\n", err)
				}
			}
		}
	}

	// Verify file exists and has content
	fileInfo, err := os.Stat(tmpFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Generated QR file not found: %v", err)})
		return
	}
	if fileInfo.Size() == 0 {
		os.Remove(tmpFile) // Clean up empty file
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Generated QR file is empty"})
		return
	}

	// Read the file and send it as requested format
	file, err := os.Open(tmpFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to read QR code file: %v", err)})
		return
	}
	defer file.Close()
	defer os.Remove(tmpFile) // Clean up temp file

	c.Header("Cache-Control", "public, max-age=3600") // Cache for 1 hour

	if outputFormat == "jpg" {
		// Decode PNG, composite onto opaque background, encode JPEG
		img, _, err := image.Decode(file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to decode QR image: %v", err)})
			return
		}
		// Create opaque background using selected bgColor (fallback to white)
		bg := color.RGBA{bgColor.R, bgColor.G, bgColor.B, 255}
		if bgColor.A == 0 {
			bg = color.RGBA{255, 255, 255, 255}
		}
		outBounds := img.Bounds()
		out := image.NewRGBA(outBounds)
		draw.Draw(out, outBounds, &image.Uniform{C: bg}, image.Point{}, draw.Src)
		draw.Draw(out, outBounds, img, outBounds.Min, draw.Over)

		c.Header("Content-Type", "image/jpeg")
		if err := jpeg.Encode(c.Writer, out, &jpeg.Options{Quality: 92}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to encode JPEG: %v", err)})
			return
		}
		fmt.Printf("[QR] sent JPG size=%s shape=%s\n", size, qrShape)
		return
	}

	// Default: stream PNG bytes
	c.Header("Content-Type", "image/png")
	if _, err := io.Copy(c.Writer, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send QR code"})
		return
	}
	fmt.Printf("[QR] sent PNG size=%s shape=%s\n", size, qrShape)
}

// generateSVGQR generates a true vector SVG QR code
func (h *Handler) generateSVGQR(c *gin.Context, qrc *qrcode.QRCode, useGradient bool, fgColor, bgColor, gradientStart, gradientMiddle, gradientEnd, borderColor color.RGBA, border int, frame string, frameWidthPercent int, size, qrShape, centerLogo string) {
	// Generate true vector SVG from QR matrix data
	if err := h.generateVectorSVG(c, qrc, useGradient, fgColor, bgColor, gradientStart, gradientMiddle, gradientEnd, borderColor, border, frame, frameWidthPercent, size, qrShape, centerLogo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate vector SVG: %v", err)})
		return
	}
}

// generateVectorSVG creates a true vector SVG QR code from matrix data
func (h *Handler) generateVectorSVG(c *gin.Context, qrc *qrcode.QRCode, useGradient bool, fgColor, bgColor, gradientStart, gradientMiddle, gradientEnd, borderColor color.RGBA, border int, frame string, frameWidthPercent int, size, qrShape, centerLogo string) error {
	// Get QR matrix dimensions and bitmap data
	dimension := qrc.Dimension()
	if dimension <= 0 {
		return fmt.Errorf("invalid QR matrix dimension")
	}

	// Access QR matrix bitmap through a different approach
	// We'll need to create a temporary bitmap by iterating through the matrix

	// Calculate module size for different target sizes
	var moduleSize int
	var targetSize int
	if size == "download" {
		targetSize = 2000
		moduleSize = targetSize / dimension
	} else {
		targetSize = 400 // Preview size
		moduleSize = targetSize / dimension
	}

	// Calculate total SVG size including padding and frame
	paddingPixels := (targetSize * border) / 100
	framePixels := 0
	if frame != "none" {
		framePixels = (targetSize * frameWidthPercent) / 100
	}

	totalSize := targetSize + (paddingPixels * 2) + (framePixels * 2)
	qrOffset := framePixels + paddingPixels

	// Start building SVG content
	svgBuilder := strings.Builder{}
	svgBuilder.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	svgBuilder.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`,
		totalSize, totalSize, totalSize, totalSize))

	// Add definitions for gradients if needed
	if useGradient {
		svgBuilder.WriteString(`<defs>`)
		svgBuilder.WriteString(`<linearGradient id="qrGradient" x1="0%" y1="0%" x2="100%" y2="100%">`)
		svgBuilder.WriteString(fmt.Sprintf(`<stop offset="0%%" stop-color="rgb(%d,%d,%d)"/>`,
			gradientStart.R, gradientStart.G, gradientStart.B))
		svgBuilder.WriteString(fmt.Sprintf(`<stop offset="50%%" stop-color="rgb(%d,%d,%d)"/>`,
			gradientMiddle.R, gradientMiddle.G, gradientMiddle.B))
		svgBuilder.WriteString(fmt.Sprintf(`<stop offset="100%%" stop-color="rgb(%d,%d,%d)"/>`,
			gradientEnd.R, gradientEnd.G, gradientEnd.B))
		svgBuilder.WriteString(`</linearGradient>`)
		svgBuilder.WriteString(`</defs>`)
	}

	// Add background
	if bgColor.A > 0 {
		svgBuilder.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="rgb(%d,%d,%d)"/>`,
			totalSize, totalSize, bgColor.R, bgColor.G, bgColor.B))
	}

	// Add frame if requested
	if frame != "none" {
		frameFillColor := fmt.Sprintf("rgb(%d,%d,%d)", borderColor.R, borderColor.G, borderColor.B)
		if useGradient {
			frameFillColor = "url(#qrGradient)"
		}

		// Create simple frame as a border (4 rectangles around the edges)
		// Top border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="0" y="0" width="%d" height="%d" fill="%s"/>`,
			totalSize, framePixels, frameFillColor))
		// Bottom border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="0" y="%d" width="%d" height="%d" fill="%s"/>`,
			totalSize-framePixels, totalSize, framePixels, frameFillColor))
		// Left border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="0" y="%d" width="%d" height="%d" fill="%s"/>`,
			framePixels, framePixels, totalSize-(2*framePixels), frameFillColor))
		// Right border
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
			totalSize-framePixels, framePixels, framePixels, totalSize-(2*framePixels), frameFillColor))
	}

	// Generate QR modules as SVG paths
	fillColor := fmt.Sprintf("rgb(%d,%d,%d)", fgColor.R, fgColor.G, fgColor.B)
	if useGradient {
		fillColor = "url(#qrGradient)"
	}

	// Create a bitmap by examining a temporary PNG first
	// Since we can't directly access the matrix, we'll generate a minimal PNG first
	tmpFile := filepath.Join(os.TempDir(), generateUniqueFilename("qr_matrix", ".png"))
	defer os.Remove(tmpFile)

	// Create minimal writer for matrix extraction
	writer, err := standard.New(tmpFile, standard.WithQRWidth(1), standard.WithBorderWidth(0), standard.WithBgColor(color.RGBA{255, 255, 255, 255}), standard.WithFgColor(color.RGBA{0, 0, 0, 255}))
	if err != nil {
		return fmt.Errorf("failed to create QR writer for matrix extraction: %v", err)
	}

	if err := qrc.Save(writer); err != nil {
		return fmt.Errorf("failed to generate QR for matrix extraction: %v", err)
	}
	writer.Close()

	// Read the generated PNG to extract the matrix
	file, err := os.Open(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to open matrix file: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode matrix image: %v", err)
	}

	bounds := img.Bounds()
	actualDimension := bounds.Dx() // Should match our calculated dimension

	// Iterate through image and create rectangles for dark pixels
	for y := 0; y < actualDimension; y++ {
		for x := 0; x < actualDimension; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r < 32768 { // Dark pixel (closer to black than white)
				// Scale from 1-pixel module to target moduleSize
				moduleX := qrOffset + (x * moduleSize)
				moduleY := qrOffset + (y * moduleSize)

				// Apply shape based on qrShape parameter
				switch qrShape {
				case "circle":
					radius := moduleSize / 2
					centerX := moduleX + radius
					centerY := moduleY + radius
					svgBuilder.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="%s"/>`,
						centerX, centerY, radius, fillColor))
				default: // rectangle
					svgBuilder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s"/>`,
						moduleX, moduleY, moduleSize, moduleSize, fillColor))
				}
			}
		}
	}

	// Add center logo if requested
	if centerLogo == "true" {
		centerX := totalSize / 2
		centerY := totalSize / 2
		logoSize := targetSize / 4 // 25% of QR size

		// Create a white background circle for the logo
		svgBuilder.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="white"/>`,
			centerX, centerY, logoSize/2+5))

		// Add logo placeholder (this would need actual logo SVG content)
		svgBuilder.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="gray" opacity="0.3"/>`,
			centerX-logoSize/2, centerY-logoSize/2, logoSize, logoSize))
	}

	// Close SVG
	svgBuilder.WriteString(`</svg>`)

	// Return SVG content
	c.Header("Content-Type", "image/svg+xml")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "image/svg+xml", []byte(svgBuilder.String()))

	return nil
}

// Helper function to parse hex color parameters
func parseColorParam(param string, defaultColor color.RGBA) color.RGBA {
	if param == "" {
		return defaultColor
	}

	// Handle transparent background
	if strings.ToLower(param) == "transparent" {
		return color.RGBA{0, 0, 0, 0} // Fully transparent
	}

	// Remove # if present
	param = strings.TrimPrefix(param, "#")

	// Ensure it's 6 characters
	if len(param) != 6 {
		return defaultColor
	}

	// Parse hex values
	r, err1 := strconv.ParseUint(param[0:2], 16, 8)
	g, err2 := strconv.ParseUint(param[2:4], 16, 8)
	b, err3 := strconv.ParseUint(param[4:6], 16, 8)

	if err1 != nil || err2 != nil || err3 != nil {
		return defaultColor
	}

	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

// Helper function to generate unique temporary filenames
func generateUniqueFilename(prefix, extension string) string {
	timestamp := time.Now().UnixNano()
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	return fmt.Sprintf("%s_%d_%x%s", prefix, timestamp, randomBytes, extension)
}

// ensureMinimumQRSize scales up QR code if it's smaller than target size
func (h *Handler) ensureMinimumQRSize(filename string, minSize int) error {
	// Open and check current size
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	currentSize := bounds.Dx() // Assuming square QR

	fmt.Printf("Current QR size: %dx%d, target: %dx%d\n", currentSize, bounds.Dy(), minSize, minSize)

	// If already large enough, no scaling needed
	if currentSize >= minSize {
		return nil
	}

	// Calculate scale factor (use nearest neighbor for QR codes to keep sharp edges)
	scaleFactor := float64(minSize) / float64(currentSize)
	newSize := int(float64(currentSize) * scaleFactor)

	fmt.Printf("Scaling QR by factor %.2f to %dx%d\n", scaleFactor, newSize, newSize)

	// Create new larger image
	scaledImg := image.NewRGBA(image.Rect(0, 0, newSize, newSize))

	// Scale using nearest neighbor (preserves sharp QR edges)
	for y := 0; y < newSize; y++ {
		for x := 0; x < newSize; x++ {
			// Map back to original coordinates
			origX := int(float64(x) / scaleFactor)
			origY := int(float64(y) / scaleFactor)

			// Ensure we don't go out of bounds
			if origX >= currentSize {
				origX = currentSize - 1
			}
			if origY >= bounds.Dy() {
				origY = bounds.Dy() - 1
			}

			// Copy pixel
			color := qrImg.At(bounds.Min.X+origX, bounds.Min.Y+origY)
			scaledImg.Set(x, y, color)
		}
	}

	// Save the scaled image back to file
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create scaled output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, scaledImg); err != nil {
		return fmt.Errorf("failed to encode scaled PNG: %v", err)
	}

	return nil
}

// ensureExactQRSize scales the QR code to exactly targetSize x targetSize using nearest neighbor.
func (h *Handler) ensureExactQRSize(filename string, targetSize int) error {
	// Open and decode current image
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}
	img, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := img.Bounds()
	currentW := bounds.Dx()
	if currentW == 0 || targetSize <= 0 {
		return nil
	}

	scale := float64(targetSize) / float64(currentW)
	// Create destination image
	dst := image.NewRGBA(image.Rect(0, 0, targetSize, targetSize))
	for y := 0; y < targetSize; y++ {
		for x := 0; x < targetSize; x++ {
			ox := int(float64(x) / scale)
			oy := int(float64(y) / scale)
			if ox >= bounds.Dx() {
				ox = bounds.Dx() - 1
			}
			if oy >= bounds.Dy() {
				oy = bounds.Dy() - 1
			}
			dst.Set(x, y, img.At(bounds.Min.X+ox, bounds.Min.Y+oy))
		}
	}

	out, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create scaled output file: %v", err)
	}
	defer out.Close()
	if err := png.Encode(out, dst); err != nil {
		return fmt.Errorf("failed to encode scaled PNG: %v", err)
	}
	return nil
}

// addAbsolutePaddingToQRFile adds consistent padding regardless of QR resolution
func (h *Handler) addAbsolutePaddingToQRFile(filename string, borderPercent, originalSize int, bgColor color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	// Calculate padding based on percentage of original QR size for consistency
	paddingPixels := (originalSize * borderPercent) / 100
	// Scale padding proportionally if QR was resized
	if bounds.Dx() != originalSize {
		scaleFactor := float64(bounds.Dx()) / float64(originalSize)
		paddingPixels = int(float64(paddingPixels) * scaleFactor)
	}

	// Create new image with padding
	newWidth := bounds.Dx() + paddingPixels*2
	newHeight := bounds.Dy() + paddingPixels*2
	paddedImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Fill with background color only if not transparent
	if bgColor.A != 0 {
		for y := 0; y < newHeight; y++ {
			for x := 0; x < newWidth; x++ {
				paddedImg.Set(x, y, bgColor)
			}
		}
	}
	// If transparent, RGBA image starts with transparent pixels by default

	// Draw original QR code in center
	draw.Draw(paddedImg, image.Rect(paddingPixels, paddingPixels, paddingPixels+bounds.Dx(), paddingPixels+bounds.Dy()), qrImg, bounds.Min, draw.Src)

	// Save the padded image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create padded output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, paddedImg); err != nil {
		return fmt.Errorf("failed to encode padded PNG: %v", err)
	}

	return nil
}

// addFrameToQRFile adds a decorative frame around the QR code
func (h *Handler) addFrameToQRFile(filename, frameType string, frameWidth int, bgColor, frameColor color.RGBA, useGradient bool, gradientStart, gradientMiddle, gradientEnd color.RGBA) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open QR file: %v", err)
	}

	qrImg, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("failed to decode QR image: %v", err)
	}

	bounds := qrImg.Bounds()
	// Use provided frameWidth parameter

	// Create new image with frame
	newWidth := bounds.Dx() + frameWidth*2
	newHeight := bounds.Dy() + frameWidth*2
	framedImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Fill with background color only if not transparent
	if bgColor.A != 0 {
		for y := 0; y < newHeight; y++ {
			for x := 0; x < newWidth; x++ {
				framedImg.Set(x, y, bgColor)
			}
		}
	}
	// If transparent, RGBA image starts with transparent pixels by default

	// Helper function to calculate gradient color at position (45-degree, bottom-left to top-right)
	calculateFrameColor := func(x, y int) color.RGBA {
		if !useGradient {
			return frameColor
		}

		// Calculate gradient position for 45-degree angle (bottom-left to top-right)
		// Normalize coordinates to 0-1 range
		normalizedX := float64(x) / float64(newWidth)
		normalizedY := float64(y) / float64(newHeight)

		// For 45-degree gradient from bottom-left to top-right:
		// t = 0 at bottom-left, t = 1 at top-right
		t := (normalizedX + (1.0 - normalizedY)) / 2.0

		// Clamp t to valid range
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}

		// Interpolate between the 3 gradient colors
		if t <= 0.5 {
			// Interpolate between start (t=0) and middle (t=0.5)
			localT := t * 2.0
			return h.lerpColor(gradientStart, gradientMiddle, localT)
		} else {
			// Interpolate between middle (t=0.5) and end (t=1.0)
			localT := (t - 0.5) * 2.0
			return h.lerpColor(gradientMiddle, gradientEnd, localT)
		}
	}

	// Helper: rounded rectangle hit test used for rounded gap shaping
	insideRoundedRect := func(x, y, left, top, right, bottom, r int) bool {
		if left > right || top > bottom {
			return false
		}
		if r <= 0 {
			return x >= left && x <= right && y >= top && y <= bottom
		}
		// Straight bands
		if x >= left+r && x <= right-r && y >= top && y <= bottom {
			return true
		}
		if y >= top+r && y <= bottom-r && x >= left && x <= right {
			return true
		}
		// Corner circles
		dx, dy := x-(left+r), y-(top+r)
		if dx*dx+dy*dy <= r*r {
			return true
		}
		dx, dy = x-(right-r), y-(top+r)
		if dx*dx+dy*dy <= r*r {
			return true
		}
		dx, dy = x-(left+r), y-(bottom-r)
		if dx*dx+dy*dy <= r*r {
			return true
		}
		dx, dy = x-(right-r), y-(bottom-r)
		return dx*dx+dy*dy <= r*r
	}

	// Draw frame border based on type
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			isFrameArea := x < frameWidth || x >= newWidth-frameWidth || y < frameWidth || y >= newHeight-frameWidth
			if !isFrameArea {
				continue
			}

			// Extract base pattern from frame type
			basePattern := frameType
			if strings.HasPrefix(frameType, "rounded-") {
				basePattern = strings.TrimPrefix(frameType, "rounded-")
			}

			switch basePattern {
			case "irregular":
				// Irregular dashed pattern - random dash lengths
				cornerSize := frameWidth

				// Draw solid corners
				if (x < cornerSize && y < cornerSize) ||
					(x >= newWidth-cornerSize && y < cornerSize) ||
					(x < cornerSize && y >= newHeight-cornerSize) ||
					(x >= newWidth-cornerSize && y >= newHeight-cornerSize) {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				} else {
					// Create irregular dashes using hash for randomness
					if (y < frameWidth || y >= newHeight-frameWidth) && x >= cornerSize && x < newWidth-cornerSize {
						hash := (x * 13) % 17
						dashLength := 4 + (hash % 8) // Random length 4-11
						gapLength := 2 + (hash % 4)  // Random gap 2-5
						total := dashLength + gapLength
						if (x-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
					if (x < frameWidth || x >= newWidth-frameWidth) && y >= cornerSize && y < newHeight-cornerSize {
						hash := (y * 13) % 17
						dashLength := 4 + (hash % 8) // Random length 4-11
						gapLength := 2 + (hash % 4)  // Random gap 2-5
						total := dashLength + gapLength
						if (y-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
				}
			case "dotted":
				// Stamp edge pattern - classic perforated postage stamp look
				perfSpacing := frameWidth
				if perfSpacing < 6 {
					perfSpacing = 6
				}
				perfRadius := frameWidth / 3
				if perfRadius < 2 {
					perfRadius = 2
				}

				// Draw solid border first
				framedImg.Set(x, y, calculateFrameColor(x, y))

				// Cut out perforations (circular holes)
				if y < frameWidth || y >= newHeight-frameWidth {
					// Top/bottom edges
					if x%perfSpacing < perfRadius*2 {
						centerX := (x/perfSpacing)*perfSpacing + perfRadius
						centerY := frameWidth / 2
						if y >= newHeight-frameWidth {
							centerY = newHeight - frameWidth/2
						}
						dx := x - centerX
						dy := y - centerY
						if dx*dx+dy*dy <= perfRadius*perfRadius {
							framedImg.Set(x, y, bgColor) // Cut hole
						}
					}
				} else if x < frameWidth || x >= newWidth-frameWidth {
					// Left/right edges
					if y%perfSpacing < perfRadius*2 {
						centerY := (y/perfSpacing)*perfSpacing + perfRadius
						centerX := frameWidth / 2
						if x >= newWidth-frameWidth {
							centerX = newWidth - frameWidth/2
						}
						dx := x - centerX
						dy := y - centerY
						if dx*dx+dy*dy <= perfRadius*perfRadius {
							framedImg.Set(x, y, bgColor) // Cut hole
						}
					}
				}
			case "dashed":
				// Dashed pattern with proportional corners
				dashLength := frameWidth * 3
				if dashLength < 6 {
					dashLength = 6
				}
				gapLength := dashLength / 2
				total := dashLength + gapLength
				cornerSize := frameWidth

				// Draw solid corners
				if (x < cornerSize && y < cornerSize) ||
					(x >= newWidth-cornerSize && y < cornerSize) ||
					(x < cornerSize && y >= newHeight-cornerSize) ||
					(x >= newWidth-cornerSize && y >= newHeight-cornerSize) {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				} else {
					// Draw dashes on edges
					if (y < frameWidth || y >= newHeight-frameWidth) && x >= cornerSize && x < newWidth-cornerSize {
						if (x-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
					if (x < frameWidth || x >= newWidth-frameWidth) && y >= cornerSize && y < newHeight-cornerSize {
						if (y-cornerSize)%total < dashLength {
							framedImg.Set(x, y, calculateFrameColor(x, y))
						}
					}
				}
			case "double":
				// Double border with optional rounded corners.
				// Split frameWidth into outer stroke | gap | inner stroke
				outerWidth := int(math.Max(2, math.Round(float64(frameWidth)*0.4)))
				gapWidth := int(math.Max(1, math.Round(float64(frameWidth)*0.2)))
				innerWidth := frameWidth - outerWidth - gapWidth
				if innerWidth < 1 {
					innerWidth = 1
				}
				// Bias inner stroke thicker without changing outer weight:
				// move a small delta from the gap to the inner band.
				delta := int(math.Max(1, math.Round(float64(frameWidth)*0.1)))
				if gapWidth > delta {
					gapWidth -= delta
					innerWidth += delta
				} else if gapWidth > 1 { // ensure at least 1px gap remains
					innerWidth += (gapWidth - 1)
					gapWidth = 1
				}
				// Adjust band balance depending on rounded vs straight.
				// Rounded: widen the transparent gap slightly by borrowing from the outer band
				// (keeps the current rounded look you liked).
				// Straight: make the outer band a bit thicker by borrowing from the gap.
				if strings.HasPrefix(frameType, "rounded-") {
					deltaGap := int(math.Max(1, math.Round(float64(frameWidth)*0.1)))
					if outerWidth > deltaGap+1 { // leave at least 1px outer stroke
						outerWidth -= deltaGap
						gapWidth += deltaGap
					}
				} else {
					deltaOuter := int(math.Max(1, math.Round(float64(frameWidth)*0.1)))
					if gapWidth > deltaOuter { // prefer to reduce gap first
						gapWidth -= deltaOuter
						outerWidth += deltaOuter
					} else if gapWidth > 1 { // ensure at least 1px gap remains
						outerWidth += (gapWidth - 1)
						gapWidth = 1
					} else if innerWidth > 1 { // as a last resort, borrow from inner minimally
						outerWidth += 1
						innerWidth -= 1
					}
					// Fine-tune: give the gap +1px from the inner band if available
					// to restore a tiny breathing room between strokes.
					if innerWidth > 2 {
						innerWidth -= 1
						gapWidth += 1
					}
				}

				if strings.HasPrefix(frameType, "rounded-") {
					// Rounded classification using rounded rectangles relative to the inner boundary.
					innerL, innerT := frameWidth, frameWidth
					innerRgt, innerBtm := newWidth-1-frameWidth, newHeight-1-frameWidth
					baseR := int(math.Round(float64(frameWidth) * 0.55)) // approximate inner corner radius

					// Offsets from inner boundary for rings
					offIn := innerWidth
					offGap := innerWidth + gapWidth
					offOut := innerWidth + gapWidth + outerWidth // should equal frameWidth

					// Precompute expanded boxes and radii
					clamp := func(v, lo, hi int) int {
						if v < lo {
							return lo
						}
						if v > hi {
							return hi
						}
						return v
					}

					// Inner stroke outer edge
					inL := clamp(innerL-offIn, 0, newWidth-1)
					inT := clamp(innerT-offIn, 0, newHeight-1)
					inR := clamp(innerRgt+offIn, 0, newWidth-1)
					inB := clamp(innerBtm+offIn, 0, newHeight-1)
					rIn := baseR + offIn

					// Gap outer edge
					gL := clamp(innerL-offGap, 0, newWidth-1)
					gT := clamp(innerT-offGap, 0, newHeight-1)
					gR := clamp(innerRgt+offGap, 0, newWidth-1)
					gB := clamp(innerBtm+offGap, 0, newHeight-1)
					rGap := baseR + offGap

					// Outer stroke outer edge (close to image bounds)
					oL := clamp(innerL-offOut, 0, newWidth-1)
					oT := clamp(innerT-offOut, 0, newHeight-1)
					oR := clamp(innerRgt+offOut, 0, newWidth-1)
					oB := clamp(innerBtm+offOut, 0, newHeight-1)
					rOut := baseR + offOut

					// Membership tests
					inInnerCore := insideRoundedRect(x, y, innerL, innerT, innerRgt, innerBtm, baseR)
					inInnerBand := insideRoundedRect(x, y, inL, inT, inR, inB, rIn) && !inInnerCore
					inGapBand := insideRoundedRect(x, y, gL, gT, gR, gB, rGap) && !insideRoundedRect(x, y, inL, inT, inR, inB, rIn)
					inOuterBand := insideRoundedRect(x, y, oL, oT, oR, oB, rOut) && !insideRoundedRect(x, y, gL, gT, gR, gB, rGap)

					if inOuterBand {
						framedImg.Set(x, y, calculateFrameColor(x, y))
					} else if inGapBand {
						framedImg.Set(x, y, bgColor)
					} else if inInnerBand {
						framedImg.Set(x, y, calculateFrameColor(x, y))
					}
				} else {
					// Straight double using edge-distance bands
					edgeDist := min4(x, y, newWidth-1-x, newHeight-1-y)
					if edgeDist < outerWidth {
						framedImg.Set(x, y, calculateFrameColor(x, y))
					} else if edgeDist < outerWidth+gapWidth {
						framedImg.Set(x, y, bgColor)
					} else if edgeDist < outerWidth+gapWidth+innerWidth {
						framedImg.Set(x, y, calculateFrameColor(x, y))
					}
				}
			case "diagonal":
				// Diagonal lines pattern
				// Increase stroke thickness so lines are more visible
				lineSpacing := frameWidth / 2
				if lineSpacing < 2 {
					lineSpacing = 2
				}
				thickness := frameWidth / 5
				if thickness < 2 {
					thickness = 2
				}
				if thickness >= lineSpacing {
					thickness = lineSpacing - 1
					if thickness < 1 {
						thickness = 1
					}
				}
				if (x+y)%lineSpacing < thickness {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				}
			case "grid":
				// Grid pattern - small squares in checkerboard
				gridSize := frameWidth / 3
				if gridSize < 2 {
					gridSize = 2
				}

				// Create checkerboard pattern
				gridX := x / gridSize
				gridY := y / gridSize

				// Alternating squares
				if (gridX+gridY)%2 == 0 {
					framedImg.Set(x, y, calculateFrameColor(x, y))
				}
			default:
				// Simple
				framedImg.Set(x, y, calculateFrameColor(x, y))
			}
		}
	}

	// Draw original QR code in center FIRST
	draw.Draw(framedImg, image.Rect(frameWidth, frameWidth, frameWidth+bounds.Dx(), frameWidth+bounds.Dy()), qrImg, bounds.Min, draw.Src)

	// Apply rounded corners if frame type starts with "rounded-"
	if strings.HasPrefix(frameType, "rounded-") {
		h.applySimpleRoundedFrame(framedImg, frameWidth, bgColor)
	}

	// Save the framed image
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create framed output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, framedImg); err != nil {
		return fmt.Errorf("failed to encode framed PNG: %v", err)
	}

	return nil
}

// applySimpleRoundedFrame applies rounded corners to frame areas only
func (h *Handler) applySimpleRoundedFrame(img *image.RGBA, frameWidth int, bgColor color.RGBA) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	// Choose radii so inner radius stays > 0 to create a rounded inner edge.
	// Make inner radius proportional to frame width, then derive outer radius
	// to keep a uniform stroke thickness (outerR - innerR == frameWidth).
	innerR := int(math.Max(2, math.Round(float64(frameWidth)*0.55)))
	outerR := innerR + frameWidth

	// Colors used when carving: outside outer rounded rect stays transparent;
	// inner carve should match the QR/padding background color so there's no white halo.
	outerClear := color.RGBA{0, 0, 0, 0}
	innerClear := bgColor
	if bgColor.A == 0 {
		innerClear = color.RGBA{0, 0, 0, 0}
	}

	// Rounded rectangle hit-test
	insideRoundedRect := func(x, y, left, top, right, bottom, r int) bool {
		if r <= 0 {
			return x >= left && x <= right && y >= top && y <= bottom
		}
		// Central bands
		if x >= left+r && x <= right-r && y >= top && y <= bottom {
			return true
		}
		if y >= top+r && y <= bottom-r && x >= left && x <= right {
			return true
		}
		// Corner circles
		// Top-left
		dx := x - (left + r)
		dy := y - (top + r)
		if dx*dx+dy*dy <= r*r {
			return true
		}
		// Top-right
		dx = x - (right - r)
		dy = y - (top + r)
		if dx*dx+dy*dy <= r*r {
			return true
		}
		// Bottom-left
		dx = x - (left + r)
		dy = y - (bottom - r)
		if dx*dx+dy*dy <= r*r {
			return true
		}
		// Bottom-right
		dx = x - (right - r)
		dy = y - (bottom - r)
		return dx*dx+dy*dy <= r*r
	}

	// Define outer and inner rounded rectangles (inclusive coordinates)
	outerL, outerT := 0, 0
	outerRgt, outerBtm := width-1, height-1
	innerL, innerT := frameWidth, frameWidth
	innerRgt, innerBtm := width-1-frameWidth, height-1-frameWidth

	// Carve a full inner sub-stroke with rounded ends by expanding the inner
	// rounded rectangle outward by a cut thickness. This removes a uniform
	// strip from the inner side of the frame, and rounds its corners.
	// Carve thickness: remove roughly a third of the band from the inner side
	// so the final rounded stroke remains bold.
	cut := int(math.Max(2, math.Ceil(float64(frameWidth)*0.33)))
	carveL, carveT := innerL-cut, innerT-cut
	carveRgt, carveBtm := innerRgt+cut, innerBtm+cut
	if carveL < 0 {
		carveL = 0
	}
	if carveT < 0 {
		carveT = 0
	}
	if carveRgt > width-1 {
		carveRgt = width - 1
	}
	if carveBtm > height-1 {
		carveBtm = height - 1
	}
	carveRadius := innerR + cut

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Only modify the frame band, never touch the QR center
			inFrame := x < frameWidth || x >= width-frameWidth || y < frameWidth || y >= height-frameWidth
			if !inFrame {
				continue
			}

			// Clear anything outside the outer rounded rectangle (outer corners)
			if !insideRoundedRect(x, y, outerL, outerT, outerRgt, outerBtm, outerR) {
				img.Set(x, y, outerClear)
				continue
			}
			// Carve a sub-stroke from the inner side with rounded corners
			if insideRoundedRect(x, y, carveL, carveT, carveRgt, carveBtm, carveRadius) {
				img.Set(x, y, innerClear)
				continue
			}
			// Else keep the pixel: it's part of the stroke (outer − inner)
		}
	}
}

// lerpColor performs linear interpolation between two colors
func (h *Handler) lerpColor(color1, color2 color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(color1.R) + t*(float64(color2.R)-float64(color1.R))),
		G: uint8(float64(color1.G) + t*(float64(color2.G)-float64(color1.G))),
		B: uint8(float64(color1.B) + t*(float64(color2.B)-float64(color1.B))),
		A: 255,
	}
}

// customShape implements the IShape interface by wrapping drawing functions from the shapes package
type customShape struct {
	drawFunc func(ctx *standard.DrawContext)
}

// Draw implements the IShape interface
func (cs *customShape) Draw(ctx *standard.DrawContext) {
	cs.drawFunc(ctx)
}

// DrawFinder implements the IShape interface for finder patterns
func (cs *customShape) DrawFinder(ctx *standard.DrawContext) {
	// Use the same drawing function for finder patterns
	cs.drawFunc(ctx)
}

// cleanupAntiAliasing removes white border pixels caused by anti-aliasing
func (h *Handler) cleanupAntiAliasing(filename string, fgColor color.RGBA) error {
	// Open and decode the image
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}

	// Convert to RGBA for manipulation
	bounds := img.Bounds()
	cleanImg := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			origColor := img.At(x, y)
			r, g, b, a := origColor.RGBA()

			// Convert back to 8-bit values
			r8, g8, b8, a8 := uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)

			// Check if this is a problematic anti-aliasing pixel
			if h.isAntiAliasingArtifact(r8, g8, b8, a8, fgColor) {
				// Make it fully transparent
				cleanImg.Set(x, y, color.RGBA{0, 0, 0, 0})
			} else {
				// Keep the original pixel
				cleanImg.Set(x, y, color.RGBA{r8, g8, b8, a8})
			}
		}
	}

	// Save the cleaned image back to file
	outFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, cleanImg); err != nil {
		return fmt.Errorf("failed to encode cleaned image: %v", err)
	}

	return nil
}

// isAntiAliasingArtifact detects semi-transparent white/gray pixels that are anti-aliasing artifacts
func (h *Handler) isAntiAliasingArtifact(r, g, b, a uint8, fgColor color.RGBA) bool {
	// Skip fully transparent pixels
	if a == 0 {
		return false
	}

	// Skip fully opaque pixels that match the foreground color (these are real QR pixels)
	if a == 255 && r == fgColor.R && g == fgColor.G && b == fgColor.B {
		return false
	}

	// Semi-transparent pixels are likely anti-aliasing artifacts
	if a < 255 {
		return true
	}

	// Light gray/white pixels that aren't the exact foreground color are likely artifacts
	if (r > 200 && g > 200 && b > 200) && (r != fgColor.R || g != fgColor.G || b != fgColor.B) {
		return true
	}

	return false
}
