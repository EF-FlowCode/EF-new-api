# 宝塔面板部署教程

本文档提供使用宝塔面板 Docker 功能部署 New API 的图文教程。

> 📖 官方文档：[宝塔面板部署](https://docs.newapi.pro/zh/docs/installation/deployment-methods/bt-docker-installation)

***

## 前置要求

| 项目    | 要求                                 |
| ----- | ---------------------------------- |
| 宝塔面板  | ≥ 9.2.0 版本                         |
| 推荐系统  | CentOS 7+、Ubuntu 18.04+、Debian 10+ |
| 服务器配置 | 至少 1 核 2G 内存                       |
| 数据库 | MySQL 8.0+ |

***

## 步骤一：安装宝塔面板

1. 前往 [宝塔面板官网](https://www.bt.cn/new/download.html) 下载适合您系统的安装脚本
2. 运行安装脚本安装宝塔面板
3. 安装完成后，使用提供的地址、用户名和密码登录宝塔面板

***

## 步骤二：安装 Docker

1. 登录宝塔面板后，在左侧菜单栏找到并点击 **Docker**
2. 首次进入会提示安装 Docker 服务，点击 **立即安装**
3. 按照提示完成 Docker 服务的安装

***

## 步骤三：安装 New API

### 方法一：使用宝塔应用商店（推荐）

1. 在宝塔面板 Docker 功能中，点击 **应用商店**
2. 搜索并找到 **New-API**
3. 点击 **安装**
4. 配置以下基本选项：
   - **容器名称**：可自定义，默认为 `new-api`
   - **端口映射**：默认为 `3000:3000`
   - **环境变量**：
     - `SQL_DSN`：MySQL 8.0+ 连接字符串（**必填**）
     - `SESSION_SECRET`：生产环境强烈建议显式设置，多机部署时必须一致
     - `CRYPTO_SECRET`：未设置时继承 `SESSION_SECRET`；多机或共享 Redis 时必须稳定且一致
5. 点击 **确认** 开始安装
6. 等待安装完成后，访问 `http://您的服务器IP:3000` 即可使用

### 方法二：使用 Docker Compose

1. 在宝塔面板中创建网站目录，如 `/www/wwwroot/new-api`
2. 创建 `docker-compose.yml` 文件：

```yaml
services:
  new-api:
    image: calciumion/new-api:latest
    container_name: new-api
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - ./data:/data
    environment:
      - SQL_DSN=root:change_me@tcp(mysql:3306)/new-api?charset=utf8mb4&parseTime=True&loc=Local
      - SESSION_SECRET=your_session_secret_here  # 请修改为随机字符串
      - TZ=Asia/Shanghai
    depends_on:
      mysql:
        condition: service_healthy

  mysql:
    image: mysql:8.4
    container_name: new-api-mysql
    restart: always
    environment:
      - MYSQL_ROOT_PASSWORD=change_me
      - MYSQL_DATABASE=new-api
      - TZ=Asia/Shanghai
    volumes:
      - mysql_data:/var/lib/mysql
    healthcheck:
      test: ["CMD-SHELL", "mysqladmin ping -h 127.0.0.1 -uroot -p$$MYSQL_ROOT_PASSWORD --silent"]
      interval: 5s
      timeout: 5s
      retries: 30

volumes:
  mysql_data:
```

1. 在终端中进入目录并启动：

```bash
cd /www/wwwroot/new-api
docker-compose up -d
```

***

## 配置说明

### 必要环境变量

| 变量名                 | 说明                 | 是否必填   |
| ------------------- | ------------------ | ------ |
| `SQL_DSN`           | MySQL 8.0+ 连接字符串 | **必填** |
| `SESSION_SECRET`    | 会话密钥；未设置时会随机生成，多机或跨重启保持会话时必须固定 | 生产强烈建议 |
| `CRYPTO_SECRET`     | 加密密钥；默认继承 `SESSION_SECRET`，多机或共享 Redis 时必须一致 | 条件必填 |
| `SQL_SLOW_THRESHOLD_MS` | 慢查询日志阈值（毫秒，`0` 表示关闭） | 可选 |
| `REDIS_CONN_STRING` | Redis 连接字符串        | 可选     |

### 生成随机密钥

```bash
# 生成 SESSION_SECRET
openssl rand -hex 16

# 或使用 Linux 命令
head -c 16 /dev/urandom | xxd -p
```

***

## 常见问题

### Q1：无法访问 3000 端口？

1. 检查服务器防火墙是否开放 3000 端口
2. 在宝塔面板 **安全** 中放行 3000 端口
3. 检查云服务器安全组是否开放端口

### Q2：登录后提示会话失效？

生产环境应显式设置稳定的 `SESSION_SECRET`；多节点必须使用相同值。

### Q3：数据如何持久化？

业务数据保存在 MySQL 中，应持久化 MySQL 数据目录并定期做数据库备份：

```yaml
volumes:
  mysql_data:
```

应用的 `/data` 目录可继续用于应用侧文件，但不能替代 MySQL 数据卷。

### Q4：如何更新版本？

```bash
# 拉取最新镜像
docker pull calciumion/new-api:latest

# 重启容器
docker-compose down && docker-compose up -d
```

***

## 相关链接

- [官方文档](https://docs.newapi.pro/zh/docs/installation)
- [环境变量配置](https://docs.newapi.pro/zh/docs/installation/config-maintenance/environment-variables)
- [常见问题](https://docs.newapi.pro/zh/docs/support/faq)
- [GitHub 仓库](https://github.com/QuantumNous/new-api)

***

## 截图示例

![宝塔面板 Docker 安装](https://github.com/user-attachments/assets/7a6fc03e-c457-45e4-b8f9-184508fc26b0)

> ⚠️ 注意：`SQL_DSN` 必须指向 MySQL 8.0+；生产环境请固定 `SESSION_SECRET` 并备份 MySQL 数据。
