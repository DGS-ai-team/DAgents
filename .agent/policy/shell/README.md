# shell 策略目录

每个 shell 各有两份名单文件：
- bash.allow.txt / bash.deny.txt
- cmd.allow.txt / cmd.deny.txt
- powershell.allow.txt / powershell.deny.txt

规则：每行一个命令首词，小写匹配；空行和 # 注释行忽略。
