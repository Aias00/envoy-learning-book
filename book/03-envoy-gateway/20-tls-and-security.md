# 第20章 TLS与安全：逐段确认信任关系

先修：HTTP、域名、Gateway listener与Secret。学习目标：区分入口TLS终止、TLS透传和上游TLS，完成带证书校验的HTTPS实验，并知道认证授权与跨域配置各自解决什么问题。安全配置必须写明“保护哪一段连接、验证谁、信任从哪里来”。

## 20.1 HTTPS终止发生在哪里

客户端通过HTTPS访问网关时，TLS先建立加密连接。网关向客户端出示服务器证书，客户端验证证书链、有效期与目标主机名；握手成功后，HTTP数据在这段连接上加密传输。若Envoy负责解密并执行HTTP路由，这就是TLS终止。

终止后，Envoy到后端是一条新的连接，其协议由上游配置决定。入口配置了HTTPS，不会自动把后端3000端口变成HTTPS。我们的基础后端仍收明文HTTP：

```text
客户端 ── TLS连接A ──> Envoy ── HTTP连接B ──> 应用
                         TLS在此终止
```

如果后端也需要加密，则形成另一段TLS连接，Envoy在那段扮演TLS客户端，需要信任后端CA并验证后端名称。TLS透传则让网关按SNI等信息转发加密字节，TLS真正终止在后端。透传情况下，网关没有解密后的HTTP内容，不能沿用普通HTTPRoute的路径、header等匹配能力。

## 20.2 证书里的名称与HTTP Host不是一回事

浏览器访问 `https://secure.book.example` 时，会使用该名称验证证书，并在TLS握手中发送SNI，之后才发送HTTP Host或:authority。证书必须在Subject Alternative Name中覆盖该名称，只写CN不足以替代现代主机名校验要求。

因此，`curl -H 'Host: secure.book.example' https://127.0.0.1:18443/` 不等价于访问正确域名：URL里的127.0.0.1仍影响SNI与证书名称检查。实验用 `--resolve secure.book.example:18443:127.0.0.1` 保持URL域名和TLS名称正确，同时把TCP连接导向本机端口。

自签证书不在默认可信根中，curl应拒绝它。教学里通过 `--cacert` 显式指定这张临时证书作为信任材料。`-k`会跳过重要校验，适合辨别故障层次的有限诊断，却不能证明证书链或名称正确；本书成功路径不使用它。

## 20.3 完成独立HTTPS入口实验

先运行第18章setup，再执行：

```bash
bash labs/03-envoy-gateway/tls.sh
```

脚本调用OpenSSL生成两天有效的临时RSA证书，包含 `DNS:secure.book.example`，把证书和私钥存为envoy-book里的 `kubernetes.io/tls` Secret book-tls，然后应用 [tls.yaml](../../labs/03-envoy-gateway/tls.yaml)。Secret以tls.crt和tls.key键保存数据；base64编码只是传输表示，不是额外加密。要限制读取Secret的RBAC权限，并按平台要求管理静态加密和备份。

Gateway book-tls的listener写HTTPS、443、hostname与 `tls.mode: Terminate`；certificateRefs引用同Namespace的Secret。HTTPRoute secure通过sectionName https挂上去，再转发到原来backend:3000。专门建立第二个Gateway，便于同时观察明文入口book与HTTPS入口book-tls，避免改变第18章的既有行为。

脚本找到TLS Gateway对应的Service，转发本机18443到Service443，随后使用类似命令验证：

```bash
curl --noproxy '*' --cacert /具体临时目录/tls.crt \
  --resolve secure.book.example:18443:127.0.0.1 \
  https://secure.book.example:18443/
```

这里 `/具体临时目录/` 是脚本打印的真实目录，不能原样复制占位符。脚本已自动执行这一验证。预期200与后端echo；证书不可信、过期、SAN不匹配都应导致校验失败。脚本结束自动关闭转发，但保留证书目录和集群对象以便观察；目录含私钥，不能提交到Git。

## 20.4 上游TLS需要独立策略

如果后端开始在8443监听TLS，可通过Service443映射到8443，并把HTTPRoute的backendRef改为该Service443。然后创建BackendTLSPolicy，目标是后端Service而不是入口listener。该tag真实实现使用Gateway API v1的BackendTLSPolicy类型，内置CRD还兼容served v1alpha3；新清单可以使用v1。

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: BackendTLSPolicy
metadata:
  name: backend-trust
  namespace: envoy-book
spec:
  targetRefs:
    - group: ""
      kind: Service
      name: tls-backend
      sectionName: https
  validation:
    caCertificateRefs:
      - group: ""
        kind: ConfigMap
        name: backend-ca
    hostname: backend.book.example
```

这是说明策略结构的片段，不能单独应用后期待成功：还需要真正启用TLS的后端、对应Service、包含ca.crt的ConfigMap与合适证书。sectionName对应Service端口名https，hostname用于后端名称校验及相应TLS行为，不要求等于Service的短名称。CA可信但名称不匹配时仍应拒绝；把CA验证与名称验证当成二选一会削弱信任。

完整操作顺序见固定版本官方Backend TLS任务。初学时先证明入口TLS终止，再增加独立的上游TLS实验，每次只增加一段新的信任关系。双向TLS还需要双方验证客户端身份，必须管理客户端证书与轮换，不是给服务器证书复制一份就算完成。

## 20.5 认证、授权、CORS分工

TLS保护连接与服务器身份，不决定用户是否有权调用删除接口。认证回答“是谁”，例如JWT签名、发行者、受众、有效期等校验；授权回答“能做什么”，可能依赖角色、租户和资源归属。把用户自己传来的x-user-id当成已验证身份，是常见信任边界错误。

SecurityPolicy能配置JWT、外部认证、授权、CORS等能力，具体字段应读该tag API与任务文档。远端JWKS或外部认证服务引入网络依赖，要明确其TLS信任、超时、故障行为与可观测性。不要把公开示例token、测试JWKS和宽松测试域名复制成生产身份系统。

CORS规定浏览器脚本在跨源场景能否读取响应，curl和服务器端程序不受浏览器同源策略同等约束。允许Origin不等于认证用户，禁止Origin也不等于网络防火墙。带凭据时尤其要精确限制Origin和允许头；用“全星号”解决浏览器报错，常会掩盖真正的身份、Cookie或代理问题。

## 20.6 从证书文件到有效listener逐层检查

TLS故障也可以拆层。先确认本机端口可连接，再确认证书Secret存在且键名正确，接着看Gateway listener的条件，最后检查客户端实际收到的证书和校验错误。TCP连接拒绝通常发生在握手之前；未知CA和名称不匹配则说明至少已经进入证书验证阶段。把这些现象都叫作“HTTPS坏了”，会掩盖处理顺序。

对脚本生成的公开证书，可以运行 `openssl x509 -in /具体临时目录/tls.crt -noout -subject -issuer -dates -ext subjectAltName` 查看主体、签发者、有效期与SAN。这条命令只读公开证书，不需要输出私钥。注意主机系统时间也参与有效期校验；如果证书尚未生效或已过期，先检查时间与证书内容，不应马上关闭验证。

私钥的流转同样值得观察。脚本在临时目录生成私钥，再提交到限定Namespace的Secret；这意味着主机目录和集群Secret都成为需要保护的位置。删除集群会删除集群里的副本，却不会删除主机临时目录。生产应使用适合平台的签发和密钥管理流程，明确续期、吊销、权限与轮换失败的处理人，不能依赖两天有效的教学自签证书。

## 20.7 安全生产边界与练习

Secret轮换要观察新证书是否被控制器接收并下发，客户端实际握手是否看到新证书；更新对象成功不等于所有连接立刻换证书，已存在连接与新连接应分别考虑。控制器RBAC、Gateway附着授权、ReferenceGrant、上游网络隔离、Admin端口访问与日志脱敏也是安全边界，HTTPS不替代它们。

练习一：curl只加正确Host头，仍报证书名称不匹配，为什么？答案：TLS发生在HTTP之前，URL域名/SNI和SAN必须匹配，使用正确URL与--resolve。

练习二：入口HTTPS返回200，能证明Envoy到Pod也加密吗？答案：不能。须检查上游TLS策略、真实上游配置和连接证据；本书tls.sh的上游仍是HTTP。

练习三：把证书Secret移到另一个Namespace，再写namespace引用就够了吗？答案：不够，还需目标侧适当ReferenceGrant、控制器读取权限及实现支持，并检查listener的ResolvedRefs等条件。推荐先保持同Namespace，减少不必要的授权面。

## 本章依据

- [v1.9.2 TLS termination任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/security/tls-termination.md)。
- [v1.9.2 Backend TLS任务](https://github.com/envoyproxy/gateway/blob/v1.9.2/site/content/en/v1.9/tasks/security/backend-tls.md)与[真实翻译实现](https://github.com/envoyproxy/gateway/blob/v1.9.2/internal/gatewayapi/backendtlspolicy.go)。
- [v1.9.2 SecurityPolicy类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/securitypolicy_types.go)与[CORS类型](https://github.com/envoyproxy/gateway/blob/v1.9.2/api/v1alpha1/cors_types.go)。
- [v1.9.2 内置Gateway API CRD](https://github.com/envoyproxy/gateway/blob/v1.9.2/charts/gateway-helm/charts/crds/crds/gatewayapi-crds.yaml)：BackendTLSPolicy v1/v1alpha3 served情况。

[上一章](19-routing-and-policies.md) · [下一章](21-observability.md)
