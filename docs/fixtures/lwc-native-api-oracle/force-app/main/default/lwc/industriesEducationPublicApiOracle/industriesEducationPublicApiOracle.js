import { LightningElement } from "lwc";
import * as api from "lightning/industriesEducationPublicApi";

export default class Oracle extends LightningElement {
  label = "lightning/industriesEducationPublicApi";
  exports = Object.keys(api || {}).join(",");
}
