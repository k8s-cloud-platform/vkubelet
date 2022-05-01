# Copyright 2022 The KCP Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# build image, run this with docker build --build-arg builder_image=<golang:x.y.z>
ARG builder_image
FROM ${builder_image} as builder
WORKDIR /workspace

# Build
ARG package=.

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY ./ ./
RUN CGO_ENABLED=0 go build -o manager ${package}

# runtime image
FROM alpine:3
WORKDIR /bin
COPY --from=builder /workspace/manager .

CMD ["manager"]
