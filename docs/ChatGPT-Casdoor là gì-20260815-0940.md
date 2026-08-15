# Casdoor là gì

**User:** Anonymous  
**Created:** 8/14/2026 20:33:13  
**Updated:** 8/14/2026 20:33:36  
**Exported:** 8/15/2026 9:40:43  
**Link:** [https://chatgpt.com/c/6a7f1910-0a74-83ec-a97d-edb51b24cfa9](https://chatgpt.com/c/6a7f1910-0a74-83ec-a97d-edb51b24cfa9)  

## Prompt:
20:33:12 14/8/2026

Casdoor  làm được gì ?

## Response:
20:33:13 14/8/2026

> **Đang tìm kiếm Casdoor official docs features identity access management SSO OAuth OIDC SAML LDAP**
>
> [Overview | Casdoor Authenticator](https://app.casdoor.org/docs/overview)
> [casdoor/casdoor: An open-source Agent-first Identity and ...](https://github.com/casdoor/casdoor)
> [Top 5 Open Source Identity and Access Management (IAM) ...](https://blog.logto.io/top-oss-iam-providers-2025)
> [Overview | Casdoor · AI-Native Identity and Access ...](https://casdoor.github.io/docs/how-to-connect/overview/)
> [Casdoor Identity Cloud](https://casdoor.com/)
> [Casdoor download](https://sourceforge.net/projects/casdoor.mirror/)
> [Casdoor | EuroStack Directory Project](https://euro-stack.com/solutions/casdoor)
> [Casdoor | Hasura Authentication Tutorial](https://hasura.io/learn/graphql/hasura-authentication/integrations/casdoor/)
> [Introduction to Casdoor IAM System | PDF | Postgre Sql](https://www.scribd.com/document/800554153/Casdoor-Docs)
> [Casdoor: Open-source IAM platform with comprehensive SSO ...](https://www.auth0alternatives.com/casdoor)
> [Built-in Organization - Casdoor](https://door.casdoor.com/)
> [Casdoor](https://docs.dokploy.com/docs/templates/casdoor)
> [Simplify Identity and Access Management with Casdoor](https://www.linkedin.com/pulse/day-123-simplify-identity-access-management-casdoor-soumya-swarup-s2bpc)
> [Casdoor is another promising open-source IAM solution ...](https://news.ycombinator.com/item?id=36387422)
> [探索Casdoor：开源身份和访问管理平台的新星](https://damodev.csdn.net/6862524aba8a30336764fd69.html)
>
> **Đã tìm kiếm 15 trang web**
>
> 
>
> Đã xử lý trong vài giây

Casdoor là một **Identity and Access Management (IAM) / Single Sign-On (SSO)** server. Nói đơn giản: nếu **Casbin chủ yếu giải quyết “user này có được phép làm action này không?”**, thì **Casdoor quản lý cả phần danh tính, đăng nhập, SSO và có thể kết hợp Casbin để phân quyền**. ([Casdoor](https://app.casdoor.org/docs/overview?utm_source=chatgpt.com))

Casdoor có thể làm các việc như:

- **Quản lý user / organization / application** tập trung.
- **Đăng nhập / đăng xuất / session / token** thay cho việc nị tự viết authentication server.
- Làm **SSO**, một tài khoản đăng nhập nhiều application.
- Hỗ trợ các chuẩn **OAuth 2.0, OpenID Connect (OIDC), SAML, CAS, LDAP, SCIM**. ([Casdoor](https://app.casdoor.org/docs/overview?utm_source=chatgpt.com))
- Hỗ trợ nhiều phương thức authentication như **WebAuthn, TOTP, MFA** và tích hợp identity provider bên ngoài. ([Casdoor](https://app.casdoor.org/docs/overview?utm_source=chatgpt.com))
- Có **Web Admin UI** để quản lý user, app, provider, organization thay vì tất cả phải config bằng code.
- Tích hợp **Casbin** để triển khai ACL/RBAC/ABAC và các policy authorization. ([GitHub](https://github.com/casdoor/casdoor?utm_source=chatgpt.com))

Trong kiến trúc backend Go mà nị đang nói mấy hôm trước, có thể hiểu nó kiểu:

```text
                    Casdoor
                       │
           Authentication / Identity
                       │
              JWT / Access Token
                       │
                       ▼
Frontend ───────► Go Backend API
                       │
                       ▼
                    Casbin
                       │
              Authorization Policy
                       │
          ┌────────────┼─────────────┐
          ▼            ▼             ▼
       Role       Permission      Resource
```

Ví dụ user đăng nhập:

```text
User
 ↓
Casdoor
 ↓
Google / Email / Password / LDAP...
 ↓
Casdoor xác thực thành công
 ↓
Access Token / JWT
 ↓
Frontend
 ↓
Authorization: Bearer xxx
 ↓
Go Backend
```

Sau đó backend có thể lấy identity:

```go
userID := claims.Sub
```

rồi kiểm tra authorization bằng Casbin:

```go
ok, err := enforcer.Enforce(
    userID,
    "invoice",
    "read",
)
```

Điểm quan trọng là **Casdoor không phải “Casbin bản mạnh hơn”**.

Hai thằng nằm ở hai tầng khác nhau:

```text
Casdoor
= "Nị là ai?"

Casbin
= "Nị được phép làm gì?"
```

Ví dụ:

```text
Casdoor
────────────────────────
User: alice
Email: alice@example.com
Organization: company-a
Authentication: Google
MFA: enabled
```

Casbin:

```text
alice → invoice:read
alice → invoice:update

bob → invoice:read
```

Casdoor thậm chí còn tích hợp Casbin để quản lý authorization policy, nên hai dự án khá bổ trợ cho nhau. ([GitHub](https://github.com/casdoor/casdoor?utm_source=chatgpt.com))

Với hệ thống **Role + Feature + Permission + Quota** mà nị đang thiết kế, ngộ sẽ **không nhét tất cả vào Casdoor**. Kiến trúc hợp lý hơn là:

```text
Casdoor
│
├── Identity
├── Login
├── SSO
├── OAuth/OIDC
├── MFA
└── Organizations

Application Backend
│
├── Role
├── Feature
├── Permission / Action
├── Subscription
└── Quota

Casbin / authorization engine
│
└── Kiểm tra policy
```

Tức là Casdoor giải quyết rất tốt bài toán mà nếu tự viết thì nị sẽ phải xây cả một đống thứ:

```text
/register
/login
/logout
/refresh-token
/reset-password
/change-password
/verify-email
/google-login
/github-login
/mfa
/session
/oauth/authorize
/oauth/token
/user-management
organization-management
...
```

Thay vào đó application của nị có thể xem **Casdoor như một auth service riêng**.

Nếu so nhanh:

| | Casbin | Casdoor |
|---|---|---|
| Authentication | ❌ | ✅ |
| User management | ❌ | ✅ |
| Login UI | ❌ | ✅ |
| OAuth/OIDC | ❌ | ✅ |
| SSO | ❌ | ✅ |
| LDAP/SAML | ❌ | ✅ |
| MFA | ❌ | ✅ |
| RBAC | ✅ | ✅ / Casbin |
| ABAC | ✅ | ✅ / Casbin |
| Policy engine | ✅ Rất mạnh | dùng Casbin |
| Quota nghiệp vụ | ❌ | ❌ nên để app |

Cho nên, nếu câu hỏi ban đầu của nị là **“có cái nào giống Casbin nhưng làm được nhiều hơn không, hình như tên Cas...?”**, thì **Casdoor rất có khả năng chính là cái nị đang nhớ**.

Nhưng nó không thay Casbin theo kiểu:

```text
Casbin → Casdoor
```

mà đúng hơn là:

```text
       Casdoor
     Authentication
          +
          │
          ▼
        Casbin
     Authorization
          +
          │
          ▼
Application Business Rules
Feature / Quota / Subscription
```

Với project Go SaaS mà nị đang thiết kế, combo này khá hợp lý.

---

**Sources:**

- [Overview | Casdoor Authenticator](https://app.casdoor.org/docs/overview?utm_source=chatgpt.com)
- [casdoor/casdoor: An open-source Agent-first Identity and ...](https://github.com/casdoor/casdoor?utm_source=chatgpt.com)



---
Powered by [ChatGPT Exporter](https://www.chatgptexporter.com)