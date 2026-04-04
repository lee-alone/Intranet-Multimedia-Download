# Cookie 加密传输功能实施总结

## 概述
基于现有 RSA 基础设施，实现了前端加密-后端解密的 Cookie 安全传输方案，使用信封加密（RSA + AES-GCM）支持任意长度的 Cookie 内容。

## 已完成的后端改造

### 1. 数据库迁移
**文件**: `migrations/20260403000000_add_user_cookies.sql`

创建了 `user_cookies` 表：
- `user_id`: 用户 ID
- `domain`: Cookie 所属域名（精确匹配）
- `content`: 解密后的原始 Cookie 内容
- `is_shared`: 是否允许其他用户共用（管理员控制）
- `updated_at`: 最后更新时间
- 唯一索引: `(user_id, domain)`

### 2. JWTManager 扩展
**文件**: `internal/auth/auth.go`

新增方法：
- `GetPublicKeyPEM() (string, error)`: 返回 PEM 格式的 RSA 公钥
- `GetPrivateKey() *rsa.PrivateKey`: 返回 RSA 私钥对象
- `GetPublicKey() *rsa.PublicKey`: 返回 RSA 公钥对象

### 3. CookieHandler 实现
**文件**: `internal/handler/cookie.go`

核心功能：
- **GetPublicKey**: 返回 RSA 公钥供前端加密使用
- **SaveCookie**: 接收加密后的 Cookie 数据，解密后保存到数据库
- **GetCookie**: 获取用户的 Cookie（用于下载引擎）
- **ListCookies**: 列出用户的所有 Cookie（仅元数据）
- **DeleteCookie**: 删除指定的 Cookie

#### 安全特性
1. **信封加密**：
   - 前端使用 RSA 公钥加密 AES Key
   - 使用 AES-GCM 加密 Cookie 内容
   - 数据格式：`[AES Key 长度 (2B)][RSA 加密的 AES Key][AES-GCM Nonce (12B)][AES-GCM 加密的内容]`

2. **Cookie 格式验证**：
   - 验证 Netscape Cookie 文件格式（7 个 Tab 分隔字段）
   - 防止注入无效内容导致下载引擎报错

3. **域名精确匹配**：
   - 防止 `abilibili.com` 匹配到 `bilibili.com`
   - 去除协议、路径、端口号和 `www.`/`m.` 前缀

4. **管理员隔离策略**：
   - 非管理员用户只能访问自己的 Cookie
   - 管理员可以设置 `is_shared = 1` 共享 Cookie

### 4. 下载引擎适配
**文件**: 
- `internal/engine/scheduler.go`
- `internal/engine/engine.go`

#### 修改内容

**TaskScheduler 新增**：
- `cookieGetter CookieGetter`: Cookie 获取接口
- `SetCookieGetter(cg CookieGetter)`: 设置 Cookie 获取器
- `createTempCookieFile(content string)`: 创建临时 Cookie 文件（权限 0600）
- `extractDomainFromURL(url string)`: 从 URL 中提取域名

**DownloadOptions 新增字段**：
- `UserID int`: 用户 ID
- `UserRole string`: 用户角色

#### 工作流程
1. 下载任务启动时，调度器从 URL 提取域名
2. 调用 `CookieGetter.GetCookieForDownload()` 获取 Cookie 内容
3. 创建临时文件（`/tmp/cookie_*.txt`），权限设置为 `0600`
4. 将临时文件路径传入 `yt-dlp --cookies` 参数
5. 下载完成后（正常或异常），defer 清理临时文件

### 5. 路由注册
**文件**: 
- `internal/server/server.go`
- `internal/server/routes.go`

#### 新增 API 端点

| 端点 | 方法 | 认证 | 说明 |
|------|------|------|------|
| `/api/v1/crypto/pubkey` | GET | ✅ | 获取 RSA 公钥（PEM 格式） |
| `/api/v1/user/cookies` | POST | ✅ | 保存加密的 Cookie |
| `/api/v1/user/cookies` | GET | ✅ | 列出用户的 Cookie（元数据） |
| `/api/v1/user/cookie?domain=xxx` | GET | ✅ | 获取指定域名的 Cookie |
| `/api/v1/user/cookie?domain=xxx` | DELETE | ✅ | 删除指定域名的 Cookie |

## 前端实现（✅ 已完成）

### ✅ 已完成的工作

#### 1. 加密工具类 - `web/src/utils/cookieCrypto.ts`
- **信封加密实现**：
  - ✅ **使用 Web Crypto API 实现 RSA-OAEP (SHA-256)**（原生浏览器支持，与后端 `rsa.DecryptOAEP(sha256.New(), ...)` **完全匹配**）
  - ✅ **使用 Web Crypto API 实现 AES-256-GCM**（浏览器原生支持）
  - ✅ **AES Key 直接传输原始 32 字节二进制数据**（不做 Base64 转换，避免密钥长度错误）
  - 数据格式完全匹配后端：`[2字节 Key 长度][RSA-OAEP 密文][12字节 IV][AES-GCM 密文 + Auth Tag]`
  
- **关键修复**：
  - ~~废弃 `jsencrypt` 库~~ → 改用 `window.crypto.subtle`（原生 RSA-OAEP + SHA-256）
  - ~~Base64 传输 AES Key~~ → 直接加密原始 32 字节 `Uint8Array`
  - `pemToArrayBuffer()`: 将 PEM 公钥转换为 SPKI 格式的 ArrayBuffer
  - `crypto.subtle.importKey('spki', ...)`: 导入公钥，指定 `{ name: 'RSA-OAEP', hash: 'SHA-256' }`
  
- **辅助功能**：
  - `extractDomainFromCookie()`: 自动从 Cookie 内容提取域名
  - `validateCookieFormat()`: 验证 Netscape 格式规范性

#### 2. API 封装 - `web/src/api/index.ts`
- `getPublicKey()`: 获取 RSA 公钥
- `saveCookie()`: 保存加密 Cookie
- `getCookies()`: 获取 Cookie 列表
- `getCookie()`: 获取指定域名 Cookie
- `deleteCookie()`: 删除指定域名 Cookie

#### 3. 视图组件 - `web/src/views/Cookies.vue`
**核心功能**：
- **状态卡片**：显示主流网站（Bilibili、YouTube、优酷、爱奇艺、腾讯视频）的 Cookie 配置状态
- **智能域名识别**：粘贴 Cookie 后自动提取域名并填入输入框
- **格式验证**：实时验证 Netscape 格式，显示错误提示
- **管理员共享开关**：admin 角色用户可设置全局共享 Cookie
- **Cookie 列表管理**：展示已保存的 Cookie（域名、更新时间、共享状态），支持删除操作
- **帮助引导**：悬浮提示如何获取 Cookie 文件（含插件下载链接）

**UI 设计**：
- 使用 Tailwind CSS 响应式布局
- 状态卡片带成功/警告图标
- 保存成功动画（✓ 图标）
- 私有/共享徽章（🔒/🔓）
- 移动端适配

#### 4. 路由和菜单 - `web/src/router/index.ts` + `web/src/layouts/MainLayout.vue`
- 路由已注册：`/cookies`
- 菜单项已添加：Cookie 管理（🔑 图标）
- 无需手动认证，继承全局 auth 守卫

### 技术栈
- **Vue 3** (Composition API + `<script setup>`)
- **TypeScript**（完整类型安全）
- **Web Crypto API**（RSA-OAEP with SHA-256 + AES-256-GCM，浏览器原生支持）
- **Tailwind CSS**（响应式样式）
- **全局 Toast 组件**（友好的成功/错误提示）

## 前端改造指南（已完成，仅供参考）

### 1. 加密工具类
```javascript
import { JSEncrypt } from 'jsencrypt';
import CryptoJS from 'crypto-js';

async function encryptCookie(content) {
    // 1. 获取公钥
    const res = await fetch('/api/v1/crypto/pubkey');
    const { pubkey } = await res.json();
    
    // 2. 生成随机 AES Key
    const aesKey = CryptoJS.lib.WordArray.random(256 / 8);
    
    // 3. 使用 AES-GCM 加密 Cookie 内容
    // 注意：CryptoJS 不支持 GCM，建议使用 Web Crypto API
    const iv = window.crypto.getRandomValues(new Uint8Array(12));
    const encodedContent = new TextEncoder().encode(content);
    
    const cryptoKey = await window.crypto.subtle.importKey(
        'raw', aesKey, 'AES-GCM', false, ['encrypt']
    );
    const encryptedContent = await window.crypto.subtle.encrypt(
        { name: 'AES-GCM', iv }, cryptoKey, encodedContent
    );
    
    // 4. 使用 RSA 公钥加密 AES Key
    const encryptor = new JSEncrypt();
    encryptor.setPublicKey(pubkey);
    const encryptedAESKey = encryptor.encrypt(CryptoJS.lib.WordArray.create(aesKey).toString());
    
    // 5. 组装信封数据
    // [AES Key 长度 (2B)][RSA 加密的 AES Key][AES-GCM Nonce (12B)][AES-GCM 加密的内容]
    const envelope = new Uint8Array([
        (encryptedAESKey.length >> 8) & 0xFF,
        encryptedAESKey.length & 0xFF,
        ...new TextEncoder().encode(encryptedAESKey),
        ...iv,
        ...new Uint8Array(encryptedContent)
    ]);
    
    return btoa(String.fromCharCode(...envelope));
}
```

### 2. UI 设计建议
- 在任务创建页面或个人中心增加"Cookie 管理"入口
- 提供 Textarea 粘贴区域，自动识别域名
- 已配置 Cookie 的下载历史显示 🔒 图标
- 支持导出/导入 Cookie 文件

## 核心安全特性

### 1. 信封加密（Hybrid Encryption）
- **为什么使用信封加密**：RSA 加密有明文长度限制（2048 位密钥约 200 字节）
- **方案**：
  - 使用 RSA-OAEP（SHA-256）加密 AES-256 Key
  - 使用 AES-GCM 加密 Cookie 内容
  - GCM 模式提供完整性保护（防篡改）

### 2. 临时文件安全
- **文件名随机**：`cookie_*.txt`（Go `os.CreateTemp` 保证唯一性）
- **文件权限**：`0600`（仅所有者可读写）
- **自动清理**：defer 确保下载后删除
- **错误处理**：清理失败记录日志，不影响主流程

### 3. 域名匹配安全
- **精确匹配**：防止 `abilibili.com` 匹配 `bilibili.com`
- **域名清洗**：
  - 去除协议（`://` 之前的部分）
  - 去除路径（第一个 `/` 之后的部分）
  - 去除端口号（`:` 之后的部分）
  - 去除 `www.` 和 `m.` 前缀
  - 统一转换为小写

### 4. Cookie 格式验证
- **Netscape 格式规范**：7 个由 Tab 分隔的字段
  ```
  domain    flag    path    secure    expiration    name    value
  ```
- **验证逻辑**：
  - 跳过空行和注释行（`#` 开头）
  - 检查字段数量（必须为 7）
  - 验证 expiration 字段为数字
  - 至少包含一条有效记录

## 使用流程

### 用户侧
1. 使用浏览器插件（如 Get cookies.txt）导出 Cookie
2. 在前端"Cookie 管理"页面粘贴内容
3. 前端自动识别域名，调用 `/api/v1/crypto/pubkey` 获取公钥
4. 使用信封加密加密 Cookie，发送到 `/api/v1/user/cookies`
5. 后端解密并验证格式，保存到数据库

### 下载侧
1. 用户发起下载请求，Task 记录 `UserID` 和 `UserRole`
2. TaskScheduler 执行下载前：
   - 从 URL 提取域名
   - 调用 `CookieGetter.GetCookieForDownload()` 获取 Cookie
   - 优先查找用户自己的 Cookie
   - 如果是管理员，回退查找共享的 Cookie
3. 创建临时文件，传入 `yt-dlp --cookies`
4. 下载完成后清理临时文件

## 待完成的工作

### 前端实现
- 引入 `jsencrypt` 和 `crypto-js` 库
- 实现信封加密逻辑
- 开发 Cookie 管理 UI
- 域名自动识别和验证

### 后端优化
- 支持管理员设置 `is_shared` 参数
- 添加 Cookie 使用审计日志
- 实现 Cookie 过期和自动清理
- 添加 API 限流和防重放攻击

### 测试
- 编写单元测试（加密/解密、域名提取、格式验证）
- 端到端测试（前端加密 → 后端解密 → 下载引擎使用）
- 安全测试（注入攻击、越权访问）

## 审核重点

完成代码后，请重点审核以下内容：

1. **RSA 解密逻辑**：
   - 填充模式是否为 OAEP with SHA-256
   - 是否使用 `crypto/rand.Reader` 作为随机源

2. **AES-GCM 实现**：
   - Nonce 长度是否为 12 字节（标准推荐）
   - 是否验证了认证标签（GCM 自动处理）

3. **数据库查询**：
   - Domain 匹配是否使用参数化查询（防 SQL 注入）
   - 唯一索引是否正确（防止重复插入）

4. **临时文件清理**：
   - defer 是否覆盖所有退出路径（正常、异常、取消）
   - 文件权限是否正确设置

5. **权限控制**：
   - 用户只能访问自己的 Cookie
   - 管理员共享 Cookie 的逻辑是否正确

## 技术栈

### 后端
- Go 1.21+
- `crypto/rsa`（RSA-OAEP）
- `crypto/aes` + `crypto/cipher`（AES-GCM）
- SQLite3（数据库）

### 前端（推荐）
- `jsencrypt`（RSA 加密）
- Web Crypto API（AES-GCM）
- Vue.js / React（UI 框架）

## 参考文档
- [RSA-OAEP 规范 (RFC 8017)](https://datatracker.ietf.org/doc/html/rfc8017)
- [AES-GCM 规范 (NIST SP 800-38D)](https://csrc.nist.gov/publications/detail/sp/800-38d/final)
- [Netscape Cookie 格式](https://curl.se/docs/http-cookies.html)
