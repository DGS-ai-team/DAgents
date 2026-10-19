//! Best-effort diagnostics for GUI-subsystem builds that may have no console.

use std::fmt;
use std::io::{self, Write};

pub(crate) fn stderr_best_effort(message: fmt::Arguments<'_>) {
    let stderr = io::stderr();
    let mut writer = stderr.lock();
    let _ = write_line(&mut writer, message);
}

fn write_line<W: Write>(writer: &mut W, message: fmt::Arguments<'_>) -> io::Result<()> {
    writer.write_fmt(message)?;
    writer.write_all(b"\n")
}

#[cfg(test)]
mod tests {
    use super::write_line;
    use std::io::{self, Write};

    struct BrokenPipeWriter;

    impl Write for BrokenPipeWriter {
        fn write(&mut self, _buf: &[u8]) -> io::Result<usize> {
            Err(io::ErrorKind::BrokenPipe.into())
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    struct ZeroProgressWriter;

    impl Write for ZeroProgressWriter {
        fn write(&mut self, _buf: &[u8]) -> io::Result<usize> {
            Ok(0)
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    #[test]
    fn diagnostic_write_preserves_message_bytes_and_newline() {
        let mut output = Vec::new();
        write_line(&mut output, format_args!("shell sse: 离线 {0}", 17)).unwrap();
        assert_eq!(output, "shell sse: 离线 17\n".as_bytes());
    }

    #[test]
    fn diagnostic_write_reports_broken_pipe_and_zero_progress() {
        assert_eq!(
            write_line(&mut BrokenPipeWriter, format_args!("diagnostic"))
                .unwrap_err()
                .kind(),
            io::ErrorKind::BrokenPipe
        );
        assert_eq!(
            write_line(&mut ZeroProgressWriter, format_args!("diagnostic"))
                .unwrap_err()
                .kind(),
            io::ErrorKind::WriteZero
        );
    }
}
