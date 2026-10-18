#!/usr/bin/env bash
# 2. Maven 构建生产包
# -Pproduction 触发 spring-boot-maven-plugin 的 repackage，产出可执行 fat jar。
set -e

export JAVA_HOME=/usr/java/jdk1.8.0_411
export PATH="$JAVA_HOME/bin:$PATH"

WORKDIR="${WORKDIR:-/srv/deploy/workspaces/tio-boot-web-hello}"
cd "$WORKDIR"

echo "工作目录 : $WORKDIR"
echo "JDK      : $(java -version 2>&1 | head -1)"
echo "Maven    : $(mvn -v 2>&1 | head -1)"
echo "提交     : $(git rev-parse --short HEAD) $(git log -1 --pretty=%s)"

# -U：强制向远程仓库核对 release 更新。
# 阿里云镜像对新发布的 nexus.io:tio-boot:2.1.7 是「先有 pom、后有 jar」的渐进缓存，
# 不加 -U 时 Maven 可能只看到本地旧状态；-U 会重新拉取，避免用到半截缓存。
mvn -U -B clean package -DskipTests -Pproduction -Dgpg.skip=true -Dmaven.javadoc.skip=true

echo '=== 构建产物 ==='
ls -lh target/tio-boot-web-hello-*.jar
