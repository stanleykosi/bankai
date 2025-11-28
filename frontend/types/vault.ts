export interface TypedDataField {
  name: string;
  type: string;
}

export interface SafeCreateTypedData {
  domain: Record<string, any>;
  types: Record<string, TypedDataField[]>;
  primaryType: string;
  message: {
    paymentToken: string;
    payment: string;
    paymentReceiver: string;
  };
}

export interface VaultDeploymentResult {
  task_id?: string;
  state?: string;
  transaction_hash?: string;
  proxy_address?: string;
}

