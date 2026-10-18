#!/usr/bin/env bash
# 2. Maven 构建生产包
# -Pproduction 触发 spring-boot-maven-plugin 的 repackage，产出可执行 fat jar。
set -e

export JAVA_HOME=/usr/java/jdk-21.0.6
export PATH="$JAVA_HOME/bin:$PATH"

WORKDIR="${WORKDIR:-/srv/worksapce/tio-websockify}"
cd "$WORKDIR"

echo "工作目录 : $WORKDIR"
echo "JDK      : $(java -version 2>&1 | head -1)"
echo "Maven    : $(mvn -v 2>&1 | head -1)"
echo "提交     : $(git rev-parse --short HEAD) $(git log -1 --pretty=%s)"

mvn -U -B clean package -DskipTests -Dgpg.skip=true -Pproduction

echo '=== 构建产物 ==='
ls -lh target/tio-websockify-*.jar