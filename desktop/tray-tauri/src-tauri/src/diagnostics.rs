//! Process output that remains safe when GUI-subsystem builds have no console.

use std::fmt;
use std::io::{self, Write};

pub(crate) fn stderr_best_effort(message: fmt::Arguments<'_>) {
    let stderr = io::stderr();
    let mut writer = stderr.lock();
    let _ = write_line(&mut writer, message);
}

pub(crate) fn stdout_line_best_effort(message: fmt::Arguments<'_>) {
    let stdout = io::stdout();
    let mut writer = stdout.lock();
    let _ = write_line(&mut writer, message);
}

pub(crate) fn stdout_prompt(message: fmt::Arguments<'_>) -> io::Result<()> {
    let stdout = io::stdout();
    let mut writer = stdout.lock();
    write_prompt(&mut writer, message)
}

fn write_raw<W: Write>(writer: &mut W, message: fmt::Arguments<'_>) -> io::Result<()> {
    writer.write_fmt(message)
}

fn write_line<W: Write>(writer: &mut W, message: fmt::Arguments<'_>) -> io::Result<()> {
    write_raw(writer, message)?;
    writer.write_all(b"\n")
}

fn write_prompt<W: Write>(writer: &mut W, message: fmt::Arguments<'_>) -> io::Result<()> {
    write_raw(writer, message)?;
    writer.flush()
}

#[cfg(test)]
mod tests {
    use super::{write_line, write_prompt, write_raw};
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

    struct FlushFailureWriter(Vec<u8>);

    impl Write for FlushFailureWriter {
        fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
            self.0.extend_from_slice(buf);
            Ok(buf.len())
        }

        fn flush(&mut self) -> io::Result<()> {
            Err(io::ErrorKind::BrokenPipe.into())
        }
    }

    #[test]
    fn diagnostic_write_preserves_message_bytes_and_newline() {
        let mut output = Vec::new();
        write_line(&mut output, format_args!("shell sse: 离线 {0}", 17)).unwrap();
        assert_eq!(output, "shell sse: 离线 17\n".as_bytes());
    }

    #[test]
    fn stdout_formats_preserve_json_newline_and_prompt_without_newline() {
        let mut json = Vec::new();
        write_line(&mut json, format_args!("{{\"running\":false}}")).unwrap();
        assert_eq!(json, b"{\"running\":false}\n");

        let mut prompt = Vec::new();
        write_raw(&mut prompt, format_args!("Upgrade now? [y/N] ")).unwrap();
        assert_eq!(prompt, b"Upgrade now? [y/N] ");
    }

    #[test]
    fn prompt_flush_failure_is_reported_to_caller() {
        let mut writer = FlushFailureWriter(Vec::new());
        let error = write_prompt(&mut writer, format_args!("Upgrade now? [y/N] ")).unwrap_err();
        assert_eq!(error.kind(), io::ErrorKind::BrokenPipe);
        assert_eq!(writer.0, b"Upgrade now? [y/N] ");
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
        assert_eq!(
            write_raw(&mut BrokenPipeWriter, format_args!("prompt"))
                .unwrap_err()
                .kind(),
            io::ErrorKind::BrokenPipe
        );
        assert_eq!(
            write_raw(&mut ZeroProgressWriter, format_args!("prompt"))
                .unwrap_err()
                .kind(),
            io::ErrorKind::WriteZero
        );
    }
}
