import { BaseService } from '@/services/base.service'

export type Bucket = {
  id: string
  name: string
  endpoint: string
  region: string
  accessKey: string
  secretKey: string
  state: string
  attached: string[]
  created: number
}

export type BucketObject = { key: string; size: number; modified: number }

export type BucketListing = { objects: BucketObject[]; folders: string[]; next: string }

class StorageService extends BaseService {
  objects(id: string, prefix: string, token?: string) {
    return this.get<BucketListing>('/storage/objects', { params: { id, prefix, token } })
  }

  upload(id: string, key: string, file: File, onProgress: (sent: number) => void) {
    return this.put('/storage/object', file, {
      params: { id, key },
      headers: { 'Content-Type': 'application/octet-stream' },
      onUploadProgress: (e) => onProgress(e.loaded),
    })
  }

  removeObject(id: string, key: string) {
    return this.delete('/storage/object', { params: { id, key } })
  }

  objectUrl(id: string, key: string) {
    return `/api/storage/object?id=${encodeURIComponent(id)}&key=${encodeURIComponent(key)}`
  }

  list() {
    return this.get<Bucket[]>('/storage')
  }

  create(name: string) {
    return this.post<Bucket>('/storage', { name })
  }

  remove(id: string) {
    return this.delete('/storage', { params: { id } })
  }

  attach(id: string, app: string) {
    return this.post('/storage/attach', undefined, { params: { id, app } })
  }

  detach(id: string, app: string) {
    return this.delete('/storage/attach', { params: { id, app } })
  }
}

export const storageService = new StorageService()
