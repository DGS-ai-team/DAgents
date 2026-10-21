#[cfg(windows)]
pub fn capture_current_screen_png() -> Result<Vec<u8>, String> {
    use image::codecs::png::PngEncoder;
    use image::{ExtendedColorType, ImageEncoder};
    use std::ffi::c_void;
    use windows::Win32::Graphics::Gdi::{
        BitBlt, CreateCompatibleBitmap, CreateCompatibleDC, DeleteDC, DeleteObject, GetDC,
        GetDIBits, ReleaseDC, SelectObject, BITMAPINFO, BITMAPINFOHEADER, BI_RGB, CAPTUREBLT,
        DIB_RGB_COLORS, SRCCOPY,
    };
    use windows::Win32::UI::WindowsAndMessaging::{
        GetSystemMetrics, SM_CXVIRTUALSCREEN, SM_CYVIRTUALSCREEN, SM_XVIRTUALSCREEN,
        SM_YVIRTUALSCREEN,
    };

    let x = unsafe { GetSystemMetrics(SM_XVIRTUALSCREEN) };
    let y = unsafe { GetSystemMetrics(SM_YVIRTUALSCREEN) };
    let width = unsafe { GetSystemMetrics(SM_CXVIRTUALSCREEN) };
    let height = unsafe { GetSystemMetrics(SM_CYVIRTUALSCREEN) };
    if width <= 0 || height <= 0 || width > 32_768 || height > 32_768 {
        return Err("无法读取当前屏幕的有效尺寸。".into());
    }
    let pixel_count = (width as usize)
        .checked_mul(height as usize)
        .ok_or_else(|| "屏幕尺寸超出截图处理范围。".to_string())?;
    if pixel_count > 50_000_000 {
        return Err("当前虚拟屏幕分辨率过大，暂不支持直接截图。".into());
    }

    unsafe {
        let screen_dc = GetDC(None);
        if screen_dc.0.is_null() {
            return Err("无法访问当前屏幕。".into());
        }
        let memory_dc = CreateCompatibleDC(Some(screen_dc));
        if memory_dc.0.is_null() {
            ReleaseDC(None, screen_dc);
            return Err("无法创建屏幕截图缓冲区。".into());
        }
        let bitmap = CreateCompatibleBitmap(screen_dc, width, height);
        if bitmap.0.is_null() {
            let _ = DeleteDC(memory_dc);
            ReleaseDC(None, screen_dc);
            return Err("无法分配屏幕截图缓冲区。".into());
        }

        let previous = SelectObject(memory_dc, bitmap.into());
        let result = if previous.0.is_null() {
            Err("无法初始化屏幕截图缓冲区。".into())
        } else {
            (|| {
                BitBlt(
                    memory_dc,
                    0,
                    0,
                    width,
                    height,
                    Some(screen_dc),
                    x,
                    y,
                    SRCCOPY | CAPTUREBLT,
                )
                .map_err(|error| format!("捕获当前屏幕失败：{error}"))?;

                // GetDIBits requires the bitmap to be deselected from the memory DC.
                SelectObject(memory_dc, previous);
                let mut info = BITMAPINFO {
                    bmiHeader: BITMAPINFOHEADER {
                        biSize: std::mem::size_of::<BITMAPINFOHEADER>() as u32,
                        biWidth: width,
                        biHeight: -height,
                        biPlanes: 1,
                        biBitCount: 32,
                        biCompression: BI_RGB.0,
                        biSizeImage: (pixel_count * 4) as u32,
                        ..Default::default()
                    },
                    ..Default::default()
                };
                let mut bgra = vec![0_u8; pixel_count * 4];
                let rows = GetDIBits(
                    screen_dc,
                    bitmap,
                    0,
                    height as u32,
                    Some(bgra.as_mut_ptr().cast::<c_void>()),
                    &mut info,
                    DIB_RGB_COLORS,
                );
                if rows != height {
                    return Err("无法读取当前屏幕的像素数据。".into());
                }

                let mut rgba = Vec::with_capacity(bgra.len());
                for pixel in bgra.chunks_exact(4) {
                    rgba.extend_from_slice(&[pixel[2], pixel[1], pixel[0], 255]);
                }
                let mut png = Vec::new();
                PngEncoder::new(&mut png)
                    .write_image(&rgba, width as u32, height as u32, ExtendedColorType::Rgba8)
                    .map_err(|error| format!("压缩屏幕截图失败：{error}"))?;
                Ok(png)
            })()
        };

        SelectObject(memory_dc, previous);
        let _ = DeleteObject(bitmap.into());
        let _ = DeleteDC(memory_dc);
        ReleaseDC(None, screen_dc);
        result
    }
}

#[cfg(not(windows))]
pub fn capture_current_screen_png() -> Result<Vec<u8>, String> {
    Err("当前 Desktop 平台尚未实现本机屏幕截图。".into())
}
