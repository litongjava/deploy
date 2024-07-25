# Deploy Documentation

`deploy` 是一个部署客户端，需要配合服务端 `deploy-server` 一起使用。其工作流程如下：

1. 上传文件到服务器的指定目录
2. 解压文件到指定目录（可选）
3. 在服务器上运行一条命令

## 构建

使用以下命令构建 `deploy` 客户端：

```shell
go build
```

或

```shell
go install
```

## 使用方法

### 方式一：通过命令行指定参数

```shell
deploy -url http://192.168.3.9:10405/deployfile/upload-run/ -file {localfile} -m {savePath} -d {unzipPath} -c "{cmd}"
```

#### 参数说明

- `-url` : 服务器完整 URL
- `-file` : 本地文件，通常是一个压缩包，上传到服务器上
- `-e` : 指定环境，默认是 `dev`
- `-a` : 指定动作，默认是 `upload-run`
- `-p` : 验证密码
- `-z` : 压缩文件目标名称和源目录，例如 `target.zip sourceDir`
- `-m` : 上传文件保存路径
- `-d` : 上传文件解压路径
- `-c` : 解压完成后执行的命令

#### 示例

```shell
deploy -url http://xxxx:10405/file/upload-run/ -file target/malang-pen-api-server-1.0.0.jar -m /data/apps/webapps/malang_pen_api_server -c "docker restart malang_pen_api_server"
```

### 配置文件.deploy.toml


`.deploy.toml` 文件用于指定不同部署动作的详细参数。每个动作有一个单独的配置块，包含以下参数：

- `url` : 服务器 URL
- `p` : 验证密码
- `file` : 本地文件路径
- `m` : 上传文件保存路径
- `d` : 上传文件解压路径（可选）
- `c` : 解压完成后执行的命令（可选）
- `b` : 打包命令（可选）
- `z` : 压缩文件目标名称和源目录（可选）

在执行 `deploy` 命令时，工具会根据配置文件中指定的参数顺序执行相应的步骤。如果命令行参数与配置文件参数冲突，命令行参数优先。


```shell
deploy -a upload-run
```
## 部署示例

#### 部署后端 Java 程序

#### 打包步骤

创建 `package-win.txt` 文件，内容如下：

```shell
set JAVA_HOME=D:\java\jdk1.8.0_121
mvn clean package -DskipTests
```

#### 部署步骤

1. 打包
2. 上传
3. 移动到指定目录
4. 执行命令

#### 示例配置文件

创建 `.deploy.toml` 文件，内容如下：

```toml
[upload-run]
b = "package-win.txt"
url = "http://192.168.1.2:10405/deploy/file/upload-run/"
p = "123456"
file = "target/malang-pen-api-server-1.0.0.jar"
m = "/data/apps/webapps/malang_pen_api_server"
c = "docker restart malang_pen_api_server"
```

#### 执行部署命令

```shell
deploy
```

### 部署前端纯静态文件

#### 打包步骤

创建 `package-win.txt` 文件，内容如下：

```shell
npm run build:prod
```

#### 部署步骤

1. 打包
2. 压缩
3. 上传
4. 解压到指定目录

#### 示例配置文件

创建 `.deploy.toml` 文件，内容如下：

```toml
[dev.upload-run]
url = "http://192.168.1.2:10405/deploy/file/upload-run/"
p = "123456"
b = "package-win.txt"
z = "dist.zip dist"
file = "dist.zip"
d = "/data/apps/bussines-web"
```

#### 执行部署命令

```shell
deploy
```
