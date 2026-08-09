package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// 托盘图标尺寸
const iconSize = 64

// 程序生成托盘图标：蓝色圆形背景 + 白色扬声器与声波，返回 ICO 格式字节
// （Vista 及以上系统支持 ICO 内嵌 PNG 数据）
func generateIconICO() ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	cx, cy := float64(iconSize)/2, float64(iconSize)/2

	// 扬声器各关键点
	const (
		spkX0, spkX1 = 13.0, 21.0 // 音箱主体矩形
		spkY0, spkY1 = 26.0, 38.0
		coneTipX     = 31.0 // 喇叭口
		coneTipY0    = 20.0
		coneTipY1    = 44.0
		waveCX       = 31.0 // 声波的圆心
		waveCY       = 32.0
	)

	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			dx, dy := fx-cx, fy-cy
			dist := math.Hypot(dx, dy)
			if dist > 30 {
				continue // 圆外保持透明
			}

			// 背景：垂直方向蓝色渐变
			t := fy / iconSize
			bg := color.NRGBA{
				R: uint8(0x2c + (0x1a-0x2c)*t),
				G: uint8(0x7c + (0x5f-0x7c)*t),
				B: uint8(0xd3 + (0xb4-0xd3)*t),
				A: 255,
			}

			// 扬声器主体（矩形）
			inSpk := fx >= spkX0 && fx <= spkX1 && fy >= spkY0 && fy <= spkY1
			// 扬声器锥体（梯形，用两条边的线性插值判断）
			if !inSpk && fx > spkX1 && fx <= coneTipX {
				r := (fx - spkX1) / (coneTipX - spkX1)
				top := spkY0 + (coneTipY0-spkY0)*r
				bottom := spkY1 + (coneTipY1-spkY1)*r
				inSpk = fy >= top && fy <= bottom
			}
			// 两道声波弧线（以喇叭口为圆心，±55°）
			inWave := false
			for _, radius := range []float64{10, 17} {
				d := math.Hypot(fx-waveCX, fy-waveCY)
				if math.Abs(d-radius) < 2 {
					ang := math.Atan2(fy-waveCY, fx-waveCX) * 180 / math.Pi
					if math.Abs(ang) < 55 {
						inWave = true
					}
				}
			}

			if inSpk || inWave {
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, err
	}
	pngData := pngBuf.Bytes()

	// 封装为 ICO：文件头(6) + 目录项(16) + PNG 数据
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))  // 保留字段
	binary.Write(&ico, binary.LittleEndian, uint16(1))  // 类型：图标
	binary.Write(&ico, binary.LittleEndian, uint16(1))  // 图像数量
	ico.WriteByte(byte(iconSize))                       // 宽（0 表示 256）
	ico.WriteByte(byte(iconSize))                       // 高
	ico.WriteByte(0)                                    // 调色板颜色数
	ico.WriteByte(0)                                    // 保留
	binary.Write(&ico, binary.LittleEndian, uint16(1))  // 颜色平面
	binary.Write(&ico, binary.LittleEndian, uint16(32)) // 位深
	binary.Write(&ico, binary.LittleEndian, uint32(len(pngData)))
	binary.Write(&ico, binary.LittleEndian, uint32(6+16)) // 数据偏移
	ico.Write(pngData)
	return ico.Bytes(), nil
}
