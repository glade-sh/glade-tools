import { LightningElement } from "lwc";
import * as api from "lightning/cmsDeliveryApi";

export default class Oracle extends LightningElement {
  label = "lightning/cmsDeliveryApi";
  exports = Object.keys(api || {}).join(",");
}
