# 第三方许可证通知

本文件保留 zkey、zwork 的 Go 发行包所用组件的许可证与原始通知。它仅适用于下列第三方组件，不代表 zKeys 自身已授予开源许可；项目自身的授权以单独的 LICENSE 为准。

## 检查范围

2026-10-23，使用 Go 1.26.10，分别设置 `CGO_ENABLED=0`、`GOOS=darwin/linux`、`GOARCH=amd64/arm64`，对 `./cmd/zkey ./cmd/zwork` 执行 `go list -deps -json`。Darwin 两个目标各列出 122 个包，Linux 两个目标各列出 124 个包；外部模块仅为下面三个 `golang.org/x/*` 模块，未选入 `vendor/*` 包。检查了这些目标选中的源码文件中的版权和许可证通知，包括 Go 标准库内的附加通知。包依赖列表不等于最终链接器保留的全部符号列表，因此下文一并保留所选标准库源码的通知。

本次检查不涵盖其他 Go 版本、构建标签、启用 CGO 或 BoringCrypto 的构建，也不构成对所有历史附件的完整许可合规保证。升级工具链、依赖或构建方式后应重新检查。

Python 参考实现使用的 `cryptography` 通过 Python 包管理器单独安装，不嵌入 Go 发行包；使用 Python 实现时，应同时保留所安装 Python 依赖发行物自身附带的许可文件。Zellij、OpenSSH 也由使用者单独安装，不随本项目二进制包分发。

## Go 与 golang.org/x 模块

以下四个实际安装源码中的 LICENSE 字节完全相同，SHA256 均为 `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`，因此合并保留一次完整文本。

| 组件 | 版本 | 原始文件 |
|---|---|---|
| Go runtime 与标准库 | 1.26.10 | `GOROOT/LICENSE` |
| golang.org/x/crypto | v0.58.0 | 模块根目录 `LICENSE` |
| golang.org/x/sys | v0.49.0 | 模块根目录 `LICENSE` |
| golang.org/x/term | v0.47.0 | 模块根目录 `LICENSE` |

```text
Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Go runtime 中的 Inferno 衍生代码

来源：Go 1.26.10 `src/runtime/memmove_amd64.s` 的文件头；保留原始注释如下。

```text
// Derived from Inferno's libkern/memmove-386.s (adapted for amd64)
// https://bitbucket.org/inferno-os/inferno-os/src/master/libkern/memmove-386.s
//
//         Copyright © 1994-1999 Lucent Technologies Inc. All rights reserved.
//         Revisions Copyright © 2000-2007 Vita Nuova Holdings Limited (www.vitanuova.com).  All rights reserved.
//         Portions Copyright 2009 The Go Authors. All rights reserved.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.  IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.
```

## Go math 中的 Sun Microsystems 衍生代码

以下通知来自 Go 1.26.10 `src/math/`。保留三个实际出现的版本；同一版本的重复文本合并列出来源。

来源：`acosh.go`, `asinh.go`, `atanh.go`, `erf.go`, `expm1.go`, `j0.go`, `j1.go`, `jn.go`, `lgamma.go`, `log.go`, `log1p.go`, `remainder.go`, `sqrt.go`。

```text
// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.
//
// Developed at SunPro, a Sun Microsystems, Inc. business.
// Permission to use, copy, modify, and distribute this
// software is freely granted, provided that this notice
// is preserved.
```

来源：`cbrt.go`。

```text
// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.
//
// Developed at SunSoft, a Sun Microsystems, Inc. business.
// Permission to use, copy, modify, and distribute this
// software is freely granted, provided that this notice
// is preserved.
```

来源：`exp.go`。

```text
// Copyright (C) 2004 by Sun Microsystems, Inc. All rights reserved.
//
// Permission to use, copy, modify, and distribute this
// software is freely granted, provided that this notice
// is preserved.
```

## Go math 中的 Cephes 衍生代码

来源：Go 1.26.10 `src/math/atan.go`、`gamma.go`、`sin.go`、`tan.go`、`tanh.go`。这些文件中的下列原始通知字节一致，合并保留如下。

```text
// Copyright 1984, 1987, 1989, 1992, 2000 by Stephen L. Moshier
//
// The readme file at http://netlib.sandia.gov/cephes/ says:
//    Some software in this archive may be from the book _Methods and
// Programs for Mathematical Functions_ (Prentice-Hall or Simon & Schuster
// International, 1989) or from the Cephes Mathematical Library, a
// commercial product. In either event, it is copyrighted by the author.
// What you see here may be used freely but it comes with no support or
// guarantee.
//
//   The two known misprints in the book are repaired here in the
// source listings for the gamma function and the incomplete beta
// integral.
//
//   Stephen L. Moshier
//   moshier@na-net.ornl.gov
```
