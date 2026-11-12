// Uploads and downloads of encrypted attachments, straight to and from the
// media bucket with presigned URLs (docs/protocol.md, Media). XMLHttpRequest
// rather than fetch, for upload progress.

import { NetworkError } from './api';

export interface Transfer {
  /** PUTs the bytes to a presigned upload URL. */
  put(url: string, body: Uint8Array<ArrayBuffer>, onProgress?: (fraction: number) => void): Promise<void>;
  /** GETs the bytes from a presigned download URL. */
  get(url: string, onProgress?: (fraction: number) => void): Promise<Uint8Array<ArrayBuffer>>;
}

function request<T>(method: 'PUT' | 'GET', url: string, body: Uint8Array<ArrayBuffer> | null, onProgress: ((f: number) => void) | undefined, result: (xhr: XMLHttpRequest) => T): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, url);
    const progress = (e: ProgressEvent) => e.lengthComputable && onProgress?.(e.loaded / e.total);
    if (body) {
      xhr.setRequestHeader('Content-Type', 'application/octet-stream');
      xhr.upload.onprogress = progress;
    } else {
      xhr.responseType = 'arraybuffer';
      xhr.onprogress = progress;
    }
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300 ? resolve(result(xhr)) : reject(new NetworkError(`media ${method} failed (${xhr.status})`)));
    xhr.onerror = () => reject(new NetworkError(`media ${method} failed`));
    xhr.ontimeout = xhr.onerror;
    xhr.send(body);
  });
}

export const xhrTransfer: Transfer = {
  put: (url, body, onProgress) => request('PUT', url, body, onProgress, () => undefined),
  get: (url, onProgress) => request('GET', url, null, onProgress, (xhr) => new Uint8Array(xhr.response as ArrayBuffer)),
};
